// Copyright 2018 The gVisor Authors.
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

// go_generics reads a Go source file and writes a new version of that file with
// a few transformations applied to each. Namely:
//
//  1. Global types can be explicitly renamed with the -t option. For example,
//     if -t=A=B is passed in, all references to A will be replaced with
//     references to B; a function declaration like:
//
//     func f(arg *A)
//
//     would be renamed to:
//
//     func f(arg *B)
//
//  2. Global type definitions and their method sets will be removed when they're
//     being renamed with -t. For example, if -t=A=B is passed in, the following
//     definition and methods that existed in the input file wouldn't exist at
//     all in the output file:
//
//     type A struct{}
//
//     func (*A) f() {}
//
//  3. All global types, variables, constants and functions (not methods) are
//     prefixed and suffixed based on the option -prefix and -suffix arguments.
//     For example, if -suffix=A is passed in, the following globals:
//
//     func f()
//     type t struct{}
//
//     would be renamed to:
//
//     func fA()
//     type tA struct{}
//
//     Some special tags are also modified. For example:
//
//     "state:.(t)"
//
//     would become:
//
//     "state:.(tA)"
//
// 4. The package is renamed to the value via the -p argument.
// 5. Value of constants can be modified with -c argument.
//
// Note that not just the top-level declarations are renamed, all references to
// them are also properly renamed as well, taking into account visibility rules
// and shadowing. For example, if -suffix=A is passed in, the following:
//
// var b = 100
//
//	func f() {
//		g(b)
//		b := 0
//		g(b)
//	}
//
// Would be replaced with:
//
// var bA = 100
//
//	func f() {
//		g(bA)
//		b := 0
//		g(b)
//	}
//
// Note that the second call to g() kept "b" as an argument because it refers to
// the local variable "b".
//
// Note that go_generics can handle anonymous fields with renamed types if
// -anon is passed in, however it does not perform strict checking on parameter
// types that share the same name as the global type and therefore will rename
// them as well.
//
// You can see an example in the tools/go_generics/generics_tests/interface test.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"

	"github.com/wilinz/gvisor/tools/go_generics/globals"
)

var (
	input        = flag.String("i", "", "input `file`")
	output       = flag.String("o", "", "output `file`")
	suffix       = flag.String("suffix", "", "`suffix` to add to each global symbol")
	prefix       = flag.String("prefix", "", "`prefix` to add to each global symbol")
	packageName  = flag.String("p", "main", "output package `name`")
	printAST     = flag.Bool("ast", false, "prints the AST")
	processAnon  = flag.Bool("anon", false, "process anonymous fields")
	types        = make(mapValue)
	consts       = make(mapValue)
	imports      = make(mapValue)
	inputSubstr  = make(mapValue)
	outputSubstr = make(mapValue)
)

// mapValue implements flag.Value. We use a mapValue flag instead of a regular
// string flag when we want to allow more than one instance of the flag. For
// example, we allow several "-t A=B" arguments, and will rename them all.
type mapValue map[string]string

func (m mapValue) String() string {
	var b bytes.Buffer
	first := true
	for k, v := range m {
		if !first {
			b.WriteRune(',')
		} else {
			first = false
		}
		b.WriteString(k)
		b.WriteRune('=')
		b.WriteString(v)
	}
	return b.String()
}

func (m mapValue) Set(s string) error {
	sep := strings.Index(s, "=")
	if sep == -1 {
		return fmt.Errorf("missing '=' from '%s'", s)
	}

	m[s[:sep]] = s[sep+1:]

	return nil
}

// stateTagRegexp matches against the 'typed' state tags.
var stateTagRegexp = regexp.MustCompile(`^(.*[^a-z0-9_])state:"\.\(([^\)]*)\)"(.*)$`)

var identifierRegexp = regexp.MustCompile(`^(.*[^a-zA-Z_])([a-zA-Z_][a-zA-Z0-9_]*)(.*)$`)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Var(types, "t", "rename type A to B when `A=B` is passed in. Multiple such mappings are allowed.")
	flag.Var(consts, "c", "reassign constant A to value B when `A=B` is passed in. Multiple such mappings are allowed.")
	flag.Var(imports, "import", "specifies the import libraries to use when types are not local. `name=path` specifies that 'name', used in types as name.type, refers to the package living in 'path'.")
	flag.Var(inputSubstr, "in-substr", "replace input sub-string A with B when `A=B` is passed in. Multiple such mappings are allowed.")
	flag.Var(outputSubstr, "out-substr", "replace output sub-string A with B when `A=B` is passed in. Multiple such mappings are allowed.")
	flag.Parse()

	if *input == "" || *output == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Parse the input file.
	fset := token.NewFileSet()
	inputBytes, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	for old, new := range inputSubstr {
		inputBytes = bytes.ReplaceAll(inputBytes, []byte(old), []byte(new))
	}
	f, err := parser.ParseFile(fset, *input, inputBytes, parser.ParseComments|parser.DeclarationErrors|parser.SpuriousErrors)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// Print the AST if requested.
	if *printAST {
		ast.Print(fset, f)
	}

	cmap := ast.NewCommentMap(fset, f, f.Comments)

	// Update imports based on what's used in types and consts.
	maps := []mapValue{types, consts}
	importDecl, err := updateImports(maps, imports)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	types = maps[0]
	consts = maps[1]

	// Reassign all specified constants.
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.GenDecl)
		if !ok || d.Tok != token.CONST {
			continue
		}

		for _, gs := range d.Specs {
			s := gs.(*ast.ValueSpec)
			for i, id := range s.Names {
				if n, ok := consts[id.Name]; ok {
					s.Values[i] = &ast.BasicLit{Value: n}
				}
			}
		}
	}

	// Go through all globals and their uses in the AST and rename the types
	// with explicitly provided names, and rename all types, variables,
	// consts and functions with the provided prefix and suffix.
	globals.Visit(fset, f, func(ident *ast.Ident, kind globals.SymKind) {
		if n, ok := types[ident.Name]; ok && kind == globals.KindType {
			ident.Name = n
		} else {
			switch kind {
			case globals.KindType, globals.KindVar, globals.KindConst, globals.KindFunction:
				if ident.Name != "_" && !(ident.Name == "init" && kind == globals.KindFunction) {
					ident.Name = *prefix + ident.Name + *suffix
				}
			case globals.KindTag:
				// Modify the state tag appropriately.
				if m := stateTagRegexp.FindStringSubmatch(ident.Name); m != nil {
					if t := identifierRegexp.FindStringSubmatch(m[2]); t != nil {
						typeName := *prefix + t[2] + *suffix
						if n, ok := types[t[2]]; ok {
							typeName = n
						}
						ident.Name = m[1] + `state:".(` + t[1] + typeName + t[3] + `)"` + m[3]
					}
				}
			}
		}
	}, *processAnon)

	// Remove the definition of all types that are being remapped.
	set := make(typeSet)
	for _, v := range types {
		set[v] = struct{}{}
	}
	removeTypes(set, f)

	// Add the new imports, if any, to the top.
	if importDecl != nil {
		newDecls := make([]ast.Decl, 0, len(f.Decls)+1)
		newDecls = append(newDecls, importDecl)
		newDecls = append(newDecls, f.Decls...)
		f.Decls = newDecls
	}

	// Update comments to remove the ones potentially associated with the
	// type T that we removed.
	f.Comments = cmap.Filter(f).Comments()

	// If there are file (package) comments, delete them.
	if f.Doc != nil {
		for i, cg := range f.Comments {
			if cg == f.Doc {
				f.Comments = append(f.Comments[:i], f.Comments[i+1:]...)
				break
			}
		}
	}

	// Write the output file.
	f.Name.Name = *packageName

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, f); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	byteBuf := buf.Bytes()
	for old, new := range outputSubstr {
		byteBuf = bytes.ReplaceAll(byteBuf, []byte(old), []byte(new))
	}

	if err := os.WriteFile(*output, byteBuf, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
