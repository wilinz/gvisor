// Copyright 2021 The gVisor Authors.
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

// Binary fieldenum emits field bitmasks for all structs in a package marked
// "+fieldenum".
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"strings"
)

var (
	outputPkg      = flag.String("pkg", "", "output package")
	outputFilename = flag.String("out", "-", "output filename")
)

func main() {
	// Parse command line arguments.
	flag.Parse()
	if len(*outputPkg) == 0 {
		log.Fatalf("-pkg must be provided")
	}
	if len(flag.Args()) == 0 {
		log.Fatalf("Input files must be provided")
	}

	// Parse input files.
	inputFiles := make([]*ast.File, 0, len(flag.Args()))
	fset := token.NewFileSet()
	for _, filename := range flag.Args() {
		f, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
		if err != nil {
			log.Fatalf("Failed to parse input file %q: %v", filename, err)
		}
		inputFiles = append(inputFiles, f)
	}

	// Determine which types are marked "+fieldenum" and will consequently have
	// code generated.
	var typeNames []string
	fieldEnumTypes := make(map[string]fieldEnumTypeInfo)
	for _, f := range inputFiles {
		for _, decl := range f.Decls {
			d, ok := decl.(*ast.GenDecl)
			if !ok || d.Tok != token.TYPE || d.Doc == nil || len(d.Specs) == 0 {
				continue
			}
			for _, l := range d.Doc.List {
				const fieldenumPrefixWithSpace = "// +fieldenum "
				if l.Text == "// +fieldenum" || strings.HasPrefix(l.Text, fieldenumPrefixWithSpace) {
					spec := d.Specs[0].(*ast.TypeSpec)
					name := spec.Name.Name
					prefix := name
					if len(l.Text) > len(fieldenumPrefixWithSpace) {
						prefix = strings.TrimSpace(l.Text[len(fieldenumPrefixWithSpace):])
					}
					st, ok := spec.Type.(*ast.StructType)
					if !ok {
						log.Fatalf("Type %s is marked +fieldenum, but is not a struct", name)
					}
					typeNames = append(typeNames, name)
					fieldEnumTypes[name] = fieldEnumTypeInfo{
						prefix:     prefix,
						structType: st,
					}
					break
				}
			}
		}
	}

	// Collect information for each type for which code is being generated.
	structInfos := make([]structInfo, 0, len(typeNames))
	needAtomic := false
	for _, typeName := range typeNames {
		typeInfo := fieldEnumTypes[typeName]
		var si structInfo
		si.name = typeName
		si.prefix = typeInfo.prefix
		for _, field := range typeInfo.structType.Fields.List {
			name := structFieldName(field)
			// If the field's type is a type that is also marked +fieldenum,
			// include a FieldSet for that type in this one's. The field must
			// be a struct by value, since if it's a pointer then that struct
			// might also point to or include this one (which would make
			// FieldSet inclusion circular). It must also be a type defined in
			// this package, since otherwise we don't know whether it's marked
			// +fieldenum. Thus, field.Type must be an identifier (rather than
			// an ast.StarExpr or SelectorExpr).
			if tident, ok := field.Type.(*ast.Ident); ok {
				if fieldTypeInfo, ok := fieldEnumTypes[tident.Name]; ok {
					fsf := fieldSetField{
						fieldName:  name,
						typePrefix: fieldTypeInfo.prefix,
					}
					si.reprByFieldSet = append(si.reprByFieldSet, fsf)
					si.allFields = append(si.allFields, fsf)
					continue
				}
			}
			si.reprByBit = append(si.reprByBit, name)
			si.allFields = append(si.allFields, fieldSetField{
				fieldName: name,
			})
			// atomicbitops import will be needed for FieldSet.Load().
			needAtomic = true
		}
		structInfos = append(structInfos, si)
	}

	// Build the output file.
	var b strings.Builder
	fmt.Fprintf(&b, "// Generated by go_fieldenum.\n\n")
	fmt.Fprintf(&b, "package %s\n\n", *outputPkg)
	if needAtomic {
		fmt.Fprintf(&b, `import "github.com/wilinz/gvisor/pkg/atomicbitops"`)
		fmt.Fprintf(&b, "\n\n")
	}
	for _, si := range structInfos {
		si.writeTo(&b)
	}

	if *outputFilename == "-" {
		// Write output to stdout.
		fmt.Printf("%s", b.String())
	} else {
		// Write output to file.
		f, err := os.OpenFile(*outputFilename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			log.Fatalf("Failed to open output file %q: %v", *outputFilename, err)
		}
		if _, err := f.WriteString(b.String()); err != nil {
			log.Fatalf("Failed to write output file %q: %v", *outputFilename, err)
		}
		f.Close()
	}
}

type fieldEnumTypeInfo struct {
	prefix     string
	structType *ast.StructType
}

// structInfo contains information about the code generated for a given struct.
type structInfo struct {
	// name is the name of the represented struct.
	name string

	// prefix is the prefix X applied to the name of each generated type and
	// constant, referred to as X in the comments below for convenience.
	prefix string

	// reprByBit contains the names of fields in X that should be represented
	// by a bit in the bit mask XFieldSet.fields, and by a bool in XFields.
	reprByBit []string

	// reprByFieldSet contains fields in X whose type is a named struct (e.g.
	// Y) that has a corresponding FieldSet type YFieldSet, and which should
	// therefore be represented by including a value of type YFieldSet in
	// XFieldSet, and a value of type YFields in XFields.
	reprByFieldSet []fieldSetField

	// allFields contains all fields in X in order of declaration. Fields in
	// reprByBit have fieldSetField.typePrefix == "".
	allFields []fieldSetField
}

type fieldSetField struct {
	fieldName  string
	typePrefix string
}

func structFieldName(f *ast.Field) string {
	if len(f.Names) != 0 {
		return f.Names[0].Name
	}
	// For embedded struct fields, the field name is the unqualified type name.
	texpr := f.Type
	for {
		switch t := texpr.(type) {
		case *ast.StarExpr:
			texpr = t.X
		case *ast.SelectorExpr:
			texpr = t.Sel
		case *ast.Ident:
			return t.Name
		default:
			panic(fmt.Sprintf("unexpected %T", texpr))
		}
	}
}

func (si *structInfo) writeTo(b *strings.Builder) {
	fmt.Fprintf(b, "// A %sField represents a field in %s.\n", si.prefix, si.name)
	fmt.Fprintf(b, "type %sField uint\n\n", si.prefix)
	if len(si.reprByBit) != 0 {
		fmt.Fprintf(b, "// %sFieldX represents %s field X.\n", si.prefix, si.name)
		fmt.Fprintf(b, "const (\n")
		fmt.Fprintf(b, "\t%sField%s %sField = iota\n", si.prefix, si.reprByBit[0], si.prefix)
		for _, fieldName := range si.reprByBit[1:] {
			fmt.Fprintf(b, "\t%sField%s\n", si.prefix, fieldName)
		}
		fmt.Fprintf(b, ")\n\n")
	}

	fmt.Fprintf(b, "// %sFields represents a set of fields in %s in a literal-friendly form.\n", si.prefix, si.name)
	fmt.Fprintf(b, "// The zero value of %sFields represents an empty set.\n", si.prefix)
	fmt.Fprintf(b, "type %sFields struct {\n", si.prefix)
	for _, fieldSetField := range si.allFields {
		if fieldSetField.typePrefix == "" {
			fmt.Fprintf(b, "\t%s bool\n", fieldSetField.fieldName)
		} else {
			fmt.Fprintf(b, "\t%s %sFields\n", fieldSetField.fieldName, fieldSetField.typePrefix)
		}
	}
	fmt.Fprintf(b, "}\n\n")

	fmt.Fprintf(b, "// %sFieldSet represents a set of fields in %s in a compact form.\n", si.prefix, si.name)
	fmt.Fprintf(b, "// The zero value of %sFieldSet represents an empty set.\n", si.prefix)
	fmt.Fprintf(b, "type %sFieldSet struct {\n", si.prefix)
	numBitmaskUint32s := (len(si.reprByBit) + 31) / 32
	for _, fieldSetField := range si.reprByFieldSet {
		fmt.Fprintf(b, "\t%s %sFieldSet\n", fieldSetField.fieldName, fieldSetField.typePrefix)
	}
	if len(si.reprByBit) != 0 {
		fmt.Fprintf(b, "\tfields [%d]atomicbitops.Uint32\n", numBitmaskUint32s)
	}
	fmt.Fprintf(b, "}\n\n")

	if len(si.reprByBit) != 0 {
		fmt.Fprintf(b, "// Contains returns true if f is present in the %sFieldSet.\n", si.prefix)
		fmt.Fprintf(b, "func (fs *%sFieldSet) Contains(f %sField) bool {\n", si.prefix, si.prefix)
		if numBitmaskUint32s == 1 {
			fmt.Fprintf(b, "\treturn fs.fields[0].RacyLoad() & (uint32(1) << uint(f)) != 0\n")
		} else {
			fmt.Fprintf(b, "\treturn fs.fields[f/32].RacyLoad() & (uint32(1) << (f%%32)) != 0\n")
		}
		fmt.Fprintf(b, "}\n\n")

		fmt.Fprintf(b, "// Add adds f to the %sFieldSet.\n", si.prefix)
		fmt.Fprintf(b, "func (fs *%sFieldSet) Add(f %sField) {\n", si.prefix, si.prefix)
		if numBitmaskUint32s == 1 {
			fmt.Fprintf(b, "\tfs.fields[0] = atomicbitops.FromUint32(fs.fields[0].RacyLoad() | (uint32(1) << uint(f)))\n")
		} else {
			fmt.Fprintf(b, "\tfs.fields[f/32] = atomicbitops.FromUint32(fs.fields[f/32].RacyLoad() | (uint32(1) << (f%%32))\n")
		}
		fmt.Fprintf(b, "}\n\n")

		fmt.Fprintf(b, "// Remove removes f from the %sFieldSet.\n", si.prefix)
		fmt.Fprintf(b, "func (fs *%sFieldSet) Remove(f %sField) {\n", si.prefix, si.prefix)
		if numBitmaskUint32s == 1 {
			fmt.Fprintf(b, "\tfs.fields[0] = atomicbitops.FromUint32(fs.fields[0].RacyLoad() &^ (uint32(1) << uint(f)))\n")
		} else {
			fmt.Fprintf(b, "\tfs.fields[f/32] = atomicbitops.FromUint32(fs.fields[f/32].RacyLoad() &^ (uint32(1) << uint(f%%32)))\n")
		}
		fmt.Fprintf(b, "}\n\n")
	}

	fmt.Fprintf(b, "// Load returns a copy of the %sFieldSet.\n", si.prefix)
	fmt.Fprintf(b, "// Load is safe to call concurrently with AddFieldsLoadable, but not Add or Remove.\n")
	fmt.Fprintf(b, "func (fs *%sFieldSet) Load() (copied %sFieldSet) {\n", si.prefix, si.prefix)
	for _, fieldSetField := range si.reprByFieldSet {
		fmt.Fprintf(b, "\tcopied.%s = fs.%s.Load()\n", fieldSetField.fieldName, fieldSetField.fieldName)
	}
	for i := 0; i < numBitmaskUint32s; i++ {
		fmt.Fprintf(b, "\tcopied.fields[%d] = atomicbitops.FromUint32(fs.fields[%d].Load())\n", i, i)
	}
	fmt.Fprintf(b, "\treturn\n")
	fmt.Fprintf(b, "}\n\n")

	fmt.Fprintf(b, "// AddFieldsLoadable adds the given fields to the %sFieldSet.\n", si.prefix)
	fmt.Fprintf(b, "// AddFieldsLoadable is safe to call concurrently with Load, but not other methods (including other calls to AddFieldsLoadable).\n")
	fmt.Fprintf(b, "func (fs *%sFieldSet) AddFieldsLoadable(fields %sFields) {\n", si.prefix, si.prefix)
	for _, fieldSetField := range si.reprByFieldSet {
		fmt.Fprintf(b, "\tfs.%s.AddFieldsLoadable(fields.%s)\n", fieldSetField.fieldName, fieldSetField.fieldName)
	}
	for _, fieldName := range si.reprByBit {
		fieldConstName := fmt.Sprintf("%sField%s", si.prefix, fieldName)
		fmt.Fprintf(b, "\tif fields.%s {\n", fieldName)
		if numBitmaskUint32s == 1 {
			fmt.Fprintf(b, "\t\tfs.fields[0].Store(fs.fields[0].RacyLoad() | (uint32(1) << uint(%s)))\n", fieldConstName)
		} else {
			fmt.Fprintf(b, "\t\tword, bit := %s/32, %s%%32\n", fieldConstName, fieldConstName)
			fmt.Fprintf(b, "\t\tfs.fields[word].Store(fs.fields[word].RacyLoad() | (uint32(1) << bit))\n")
		}
		fmt.Fprintf(b, "\t}\n")
	}
	fmt.Fprintf(b, "}\n\n")
}
