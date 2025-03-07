// Copyright 2024 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package stablediffusion provides utilities to generate images with
// Stable Diffusion.
package stablediffusion

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"strings"
	"time"

	"github.com/wilinz/gvisor/pkg/test/dockerutil"
	"github.com/wilinz/gvisor/pkg/test/testutil"
)

// ContainerRunner is an interface to run containers.
type ContainerRunner interface {
	// Run runs a container with the given image and arguments to completion,
	// and returns its stdout/stderr streams as two byte strings.
	Run(ctx context.Context, image string, argv []string) ([]byte, []byte, error)
}

// dockerRunner runs Docker containers on the local machine.
type dockerRunner struct {
	logger testutil.Logger
}

// Run implements `ContainerRunner.Run`.
func (dr *dockerRunner) Run(ctx context.Context, image string, argv []string) ([]byte, []byte, error) {
	cont := dockerutil.MakeContainer(ctx, dr.logger)
	defer cont.CleanUp(ctx)
	opts, err := dockerutil.GPURunOpts(dockerutil.SniffGPUOpts{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get GPU run options: %w", err)
	}
	opts.Image = image
	if err := cont.Spawn(ctx, opts, argv...); err != nil {
		return nil, nil, fmt.Errorf("could not start Stable Diffusion container: %v", err)
	}
	waitErr := cont.Wait(ctx)
	stdout, stderr, streamsErr := cont.OutputStreams(ctx)
	if waitErr != nil {
		if streamsErr == nil {
			return nil, nil, fmt.Errorf("container exited with error: %v; stderr: %v", waitErr, stderr)
		}
		return nil, nil, fmt.Errorf("container exited with error: %v (cannot get output streams: %v)", waitErr, streamsErr)
	}
	if streamsErr != nil {
		return nil, nil, fmt.Errorf("could not get container output streams: %v", streamsErr)
	}
	return []byte(stdout), []byte(stderr), nil
}

// XL generates images using Stable Diffusion XL.
type XL struct {
	image  string
	runner ContainerRunner
}

// NewXL returns a new Stable Diffusion XL generator.
func NewXL(sdxlImage string, runner ContainerRunner) *XL {
	return &XL{
		image:  sdxlImage,
		runner: runner,
	}
}

// NewDockerXL returns a new Stable Diffusion XL generator using Docker
// containers on the local machine.
func NewDockerXL(logger testutil.Logger) *XL {
	return NewXL("gpu/stable-diffusion-xl", &dockerRunner{logger: logger})
}

// XLPrompt is the input to Stable Diffusion XL to generate an image.
type XLPrompt struct {
	// Query is the text query to generate the image with.
	Query string

	// AllowCPUOffload is whether to allow offloading parts of the model to CPU.
	AllowCPUOffload bool

	// UseRefiner is whether to use the refiner model after the base model.
	// This takes more VRAM and more time but produces a better image.
	UseRefiner bool

	// NoiseFraction is the fraction of noise to seed the image with.
	// Must be between 0.0 and 1.0 inclusively.
	NoiseFraction float64

	// Steps is the number of diffusion steps to run for the base and refiner
	// models. More steps generally means sharper results but more time to
	// generate the image. A reasonable value is between 30 and 50.
	Steps int

	// Warm controls whether the image will be generated while the model is
	// warm. This will double the running time, as the image will still be
	// generated with a cold model first.
	Warm bool
}

// xlImageJSON is the JSON response from the Stable Diffusion XL
// container's generate_image.py.
// Warm* fields are only present when `XLPrompt.Warm` is set.
type xlImageJSON struct {
	ImageASCIIBase64 []string  `json:"image_ascii_base64"`
	ImagePNGBase64   []string  `json:"image_png_base64"`
	Start            time.Time `json:"start"`
	ColdStartImage   time.Time `json:"cold_start_image"`
	ColdBaseDone     time.Time `json:"cold_base_done"`
	ColdRefinerDone  time.Time `json:"cold_refiner_done"`
	WarmStartImage   time.Time `json:"warm_start_image"`
	WarmBaseDone     time.Time `json:"warm_base_done"`
	WarmRefinerDone  time.Time `json:"warm_refiner_done"`
	Done             time.Time `json:"done"`
}

// XLImage is an image generated by Stable Diffusion XL.
type XLImage struct {
	Prompt *XLPrompt
	data   xlImageJSON
}

// ASCII returns an ASCII version of the generated image.
func (i *XLImage) ASCII() (string, error) {
	ascii, err := base64.StdEncoding.DecodeString(strings.Join(i.data.ImageASCIIBase64, ""))
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	return string(ascii), nil
}

// Image returns the generated image.
func (i *XLImage) Image() (image.Image, error) {
	return png.Decode(base64.NewDecoder(base64.StdEncoding, bytes.NewBufferString(strings.Join(i.data.ImagePNGBase64, ""))))
}

// TotalDuration returns the total time taken to generate the image.
func (i *XLImage) TotalDuration() time.Duration {
	return i.data.Done.Sub(i.data.Start)
}

// ColdBaseDuration returns time taken to run the base image generation model
// the first time the image was generated (i.e. the model was cold).
func (i *XLImage) ColdBaseDuration() time.Duration {
	return i.data.ColdBaseDone.Sub(i.data.ColdStartImage)
}

// ColdRefinerDuration returns time taken to run the refiner model
// the first time the image was generated (i.e. the model was cold).
// Returns -1 if the refiner was not run.
func (i *XLImage) ColdRefinerDuration() time.Duration {
	if !i.Prompt.UseRefiner {
		return -1
	}
	return i.data.ColdRefinerDone.Sub(i.data.ColdBaseDone)
}

// WarmBaseDuration returns time taken to run the base image generation model
// the second time the image was generated (i.e. the model was warm).
func (i *XLImage) WarmBaseDuration() time.Duration {
	return i.data.WarmBaseDone.Sub(i.data.WarmStartImage)
}

// WarmRefinerDuration returns time taken to run the refiner model
// the second time the image was generated (i.e. the model was warm).
// Returns -1 if the refiner was not run.
func (i *XLImage) WarmRefinerDuration() time.Duration {
	if !i.Prompt.UseRefiner {
		return -1
	}
	return i.data.WarmRefinerDone.Sub(i.data.WarmBaseDone)
}

// Generate generates an image with Stable Diffusion XL.
func (xl *XL) Generate(ctx context.Context, prompt *XLPrompt) (*XLImage, error) {
	argv := []string{
		"--format=METRICS",
		fmt.Sprintf("--steps=%d", prompt.Steps),
		fmt.Sprintf("--noise_frac=%f", prompt.NoiseFraction),
		"--quiet_stderr",
	}
	if prompt.AllowCPUOffload {
		argv = append(argv, "--enable_model_cpu_offload")
	}
	if prompt.UseRefiner {
		argv = append(argv, "--enable_refiner")
	}
	if prompt.Warm {
		argv = append(argv, "--warm")
	}
	argv = append(argv, prompt.Query)
	stdout, stderr, err := xl.runner.Run(ctx, xl.image, argv)
	if err != nil {
		return nil, err
	}
	xlImage := &XLImage{Prompt: prompt}
	if err := json.Unmarshal(stdout, &xlImage.data); err != nil {
		return nil, fmt.Errorf("malformed JSON output %q: %w; stderr: %v", string(stdout), err, string(stderr))
	}
	return xlImage, nil
}
