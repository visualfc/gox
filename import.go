/*
 Copyright 2021 The GoPlus Authors (goplus.org)
 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
     http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package gox

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log"
	"strconv"
	"strings"
)

// ----------------------------------------------------------------------------

// Ref type
type Ref = types.Object

// A PkgRef describes a Go package imported by others.
type PkgRef struct {
	// Types provides type information for the package.
	// The NeedTypes LoadMode bit sets this field for packages matching the
	// patterns; type information for dependencies may be missing or incomplete,
	// unless NeedDeps and NeedImports are also set.
	Types *types.Package

	nameRefs []*ast.Ident // for internal use

	isForceUsed bool // this package is force-used
	isUsed      bool
}

func (p *PkgRef) markUsed(v *ast.Ident) {
	if p.isUsed {
		return
	}
	for _, ref := range p.nameRefs {
		if ref == v {
			p.isUsed = true
			return
		}
	}
}

// Path returns the package path.
func (p *PkgRef) Path() string {
	return p.Types.Path()
}

// Ref returns the object in this package with the given name if such an
// object exists; otherwise it panics.
func (p *PkgRef) Ref(name string) Ref {
	if o := p.TryRef(name); o != nil {
		return o
	}
	panic(p.Path() + "." + name + " not found")
}

// TryRef returns the object in this package with the given name if such an
// object exists; otherwise it returns nil.
func (p *PkgRef) TryRef(name string) Ref {
	return p.Types.Scope().Lookup(name)
}

// MarkForceUsed marks this package is force-used.
func (p *PkgRef) MarkForceUsed() {
	p.isForceUsed = true
}

// EnsureImported ensures this package is imported.
func (p *PkgRef) EnsureImported() {
}

func shouldAddGopPkg(pkg *Package) bool {
	return pkg.isGopPkg && pkg.Types.Scope().Lookup(gopPackage) == nil
}

func isGopFunc(name string) bool {
	return isOverloadFunc(name) || isGoptFunc(name)
}

func isGoptFunc(name string) bool {
	return strings.HasPrefix(name, goptPrefix)
}

func isOverloadFunc(name string) bool {
	n := len(name)
	return n > 3 && name[n-3:n-1] == "__"
}

func initThisGopPkg(pkg *types.Package) {
	scope := pkg.Scope()
	if scope.Lookup(gopPackage) == nil { // not is a Go+ package
		return
	}
	if debugImport {
		log.Println("==> Import", pkg.Path())
	}
	type omthd struct {
		named *types.Named
		mthd  string
	}
	overloads := make(map[string][]types.Object)
	moverloads := make(map[omthd][]types.Object)
	names := scope.Names()
	for _, name := range names {
		o := scope.Lookup(name)
		if tn, ok := o.(*types.TypeName); ok && tn.IsAlias() {
			continue
		}
		if isOverloadFunc(name) { // overload function
			key := name[:len(name)-3]
			overloads[key] = append(overloads[key], o)
		} else if named, ok := o.Type().(*types.Named); ok {
			for i, n := 0, named.NumMethods(); i < n; i++ {
				m := named.Method(i)
				mName := m.Name()
				if isOverloadFunc(mName) { // overload method
					mthd := mName[:len(mName)-3]
					key := omthd{named, mthd}
					moverloads[key] = append(moverloads[key], m)
				}
			}
		} else {
			checkTemplateMethod(pkg, name, o)
		}
	}
	for key, items := range overloads {
		off := len(key) + 2
		fns := overloadFuncs(off, items)
		if debugImport {
			log.Println("==> NewOverloadFunc", key)
		}
		o := NewOverloadFunc(token.NoPos, pkg, key, fns...)
		scope.Insert(o)
		checkTemplateMethod(pkg, key, o)
	}
	for key, items := range moverloads {
		off := len(key.mthd) + 2
		fns := overloadFuncs(off, items)
		if debugImport {
			log.Println("==> NewOverloadMethod", key.named.Obj().Name(), key.mthd)
		}
		NewOverloadMethod(key.named, token.NoPos, pkg, key.mthd, fns...)
	}
}

const (
	goptPrefix = "Gopt_"
	gopPackage = "GopPackage"
)

func checkTemplateMethod(pkg *types.Package, name string, o types.Object) {
	if strings.HasPrefix(name, goptPrefix) {
		name = name[len(goptPrefix):]
		if pos := strings.Index(name, "_"); pos > 0 {
			tname, mname := name[:pos], name[pos+1:]
			if tobj := pkg.Scope().Lookup(tname); tobj != nil {
				if tn, ok := tobj.(*types.TypeName); ok {
					if t, ok := tn.Type().(*types.Named); ok {
						if debugImport {
							log.Println("==> NewTemplateRecvMethod", tname, mname)
						}
						NewTemplateRecvMethod(t, token.NoPos, pkg, mname, o)
					}
				}
			}
		}
	}
}

func overloadFuncs(off int, items []types.Object) []types.Object {
	fns := make([]types.Object, len(items))
	for _, item := range items {
		idx := toIndex(item.Name()[off])
		if idx >= len(items) {
			log.Panicln("overload function must be from 0 to N:", item.Name(), len(fns))
		}
		if fns[idx] != nil {
			panic("overload function exists?")
		}
		fns[idx] = item
	}
	return fns
}

func toIndex(c byte) int {
	if c >= '0' && c <= '9' {
		return int(c - '0')
	}
	if c >= 'a' && c <= 'z' {
		return int(c - ('a' - 10))
	}
	panic("invalid character out of [0-9,a-z]")
}

// ----------------------------------------------------------------------------

// Context represents all things between packages.
type Context struct {
	chkGopImports map[string]bool
}

func NewContext() *Context {
	return &Context{
		chkGopImports: make(map[string]bool),
	}
}

// InitGopPkg initializes a Go+ packages.
func (p *Context) InitGopPkg(importer types.Importer, pkgImp *types.Package) {
	pkgPath := pkgImp.Path()
	if stdpkg[pkgPath] || p.chkGopImports[pkgPath] {
		return
	}
	if !pkgImp.Complete() {
		importer.Import(pkgPath)
	}
	initThisGopPkg(pkgImp)
	p.chkGopImports[pkgPath] = true
	for _, imp := range pkgImp.Imports() {
		p.InitGopPkg(importer, imp)
	}
}

// ----------------------------------------------------------------------------

// Import imports a package by pkgPath. It will panic if pkgPath not found.
func (p *Package) Import(pkgPath string, src ...ast.Node) *PkgRef {
	return p.file.importPkg(p, pkgPath, getSrc(src))
}

// TryImport imports a package by pkgPath. It returns nil if pkgPath not found.
func (p *Package) TryImport(pkgPath string) *PkgRef {
	defer func() {
		recover()
	}()
	return p.file.importPkg(p, pkgPath, nil)
}

func (p *Package) big() *PkgRef {
	return p.file.big(p)
}

func (p *Package) unsafe() *PkgRef {
	return p.file.unsafe(p)
}

// ----------------------------------------------------------------------------

type null struct{}
type autoNames struct {
	gbl     *types.Scope
	builtin *types.Scope
	names   map[string]null
	idx     int
}

const (
	goxAutoPrefix = "_autoGo_"
)

func (p *Package) autoName() string {
	p.autoIdx++
	return goxAutoPrefix + strconv.Itoa(p.autoIdx)
}

func (p *Package) newAutoNames() *autoNames {
	return &autoNames{
		gbl:     p.Types.Scope(),
		builtin: p.builtin.Scope(),
		names:   make(map[string]null),
	}
}

func scopeHasName(at *types.Scope, name string) bool {
	if at.Lookup(name) != nil {
		return true
	}
	for i := at.NumChildren(); i > 0; {
		i--
		if scopeHasName(at.Child(i), name) {
			return true
		}
	}
	return false
}

func (p *autoNames) importHasName(name string) bool {
	_, ok := p.names[name]
	return ok
}

func (p *autoNames) hasName(name string) bool {
	return scopeHasName(p.gbl, name) || p.importHasName(name) ||
		p.builtin.Lookup(name) != nil || types.Universe.Lookup(name) != nil
}

func (p *autoNames) RequireName(name string) (ret string, renamed bool) {
	ret = name
	for p.hasName(ret) {
		p.idx++
		ret = name + strconv.Itoa(p.idx)
		renamed = true
	}
	p.names[name] = null{}
	return
}

type ImportError struct {
	Fset dbgPositioner
	Pos  token.Pos
	Path string
	Err  error
}

func (p *ImportError) Unwrap() error {
	return p.Err
}

func (p *ImportError) Error() string {
	if p.Pos == token.NoPos {
		return fmt.Sprintf("%v", p.Err)
	}
	pos := p.Fset.Position(p.Pos)
	return fmt.Sprintf("%v: %v", pos, p.Err)
}

// ----------------------------------------------------------------------------
