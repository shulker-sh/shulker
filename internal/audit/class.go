package audit

import (
	"archive/zip"
	"bytes"
	"context"
	"maps"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"
	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/out"
)

// ClassReport is what audit class shows of one class, the way javap would, with no decompiler.
type ClassReport struct {
	Name string `json:"name"`
	// Jar is the nested jar the class was found in, empty for the outermost.
	Jar        Untrusted     `json:"jar,omitempty"`
	Class      Untrusted     `json:"class"`
	Super      Untrusted     `json:"super"`
	Interfaces []Untrusted   `json:"interfaces"`
	Access     []string      `json:"access"`
	Fields     []ClassField  `json:"fields"`
	Methods    []ClassMethod `json:"methods"`
}

// ClassField is a field a class declares, with the string a constant one holds.
type ClassField struct {
	Access     []string  `json:"access"`
	Name       Untrusted `json:"name"`
	Descriptor Untrusted `json:"descriptor"`
	Constant   Untrusted `json:"constant,omitempty"`
}

// ClassMethod is a method a class declares, with what its bytecode calls, touches and loads.
type ClassMethod struct {
	Access     []string    `json:"access"`
	Name       Untrusted   `json:"name"`
	Descriptor Untrusted   `json:"descriptor"`
	Calls      []MemberRef `json:"calls"`
	Fields     []MemberRef `json:"fields"`
	Strings    []Untrusted `json:"strings"`
}

// MemberRef is a method or field an instruction names, its owner written as Java source writes a
// class name. An invokedynamic names no owner.
type MemberRef struct {
	Op         string    `json:"op"`
	Owner      Untrusted `json:"owner,omitempty"`
	Name       Untrusted `json:"name"`
	Descriptor Untrusted `json:"descriptor"`
}

// Text is the ref as one line for a terminal, as refText writes it.
func (m MemberRef) Text() string { return Untrusted(refText(m)).Line() }

type accessFlag struct {
	bit  int
	name string
}

var (
	classFlags  = []accessFlag{{0x1, "public"}, {0x10, "final"}, {0x200, "interface"}, {0x400, "abstract"}, {0x1000, "synthetic"}, {0x2000, "annotation"}, {0x4000, "enum"}}
	fieldFlags  = []accessFlag{{0x1, "public"}, {0x2, "private"}, {0x4, "protected"}, {0x8, "static"}, {0x10, "final"}, {0x40, "volatile"}, {0x80, "transient"}, {0x1000, "synthetic"}, {0x4000, "enum"}}
	methodFlags = []accessFlag{{0x1, "public"}, {0x2, "private"}, {0x4, "protected"}, {0x8, "static"}, {0x10, "final"}, {0x20, "synchronized"}, {0x40, "bridge"}, {0x80, "varargs"}, {0x100, "native"}, {0x400, "abstract"}, {0x1000, "synthetic"}}
)

func modifiers(access int, flags []accessFlag) []string {
	found := []string{}
	for _, f := range flags {
		if access&f.bit != 0 {
			found = append(found, f.name)
		}
	}
	return found
}

// classPath is the file a class name names inside a jar: a.b.C, a/b/C and a/b/C.class all name
// a/b/C.class.
func classPath(name string) string {
	name = strings.TrimSuffix(name, ".class")
	return strings.ReplaceAll(name, ".", "/") + ".class"
}

// InspectClass reads one class from s's jar. The class is looked for in the jar, then in each
// nested jar in turn, unless its name starts with a nested jar's path and "!/".
func InspectClass(s Subject, name string) (*ClassReport, error) {
	_, zr, err := readJar(s)
	if err != nil {
		return nil, err
	}
	jar, name := "", strings.TrimPrefix(name, "/")
	if i := strings.LastIndex(name, nestedSeparator); i >= 0 {
		jar, name = name[:i], name[i+len(nestedSeparator):]
		for _, part := range strings.Split(jar, nestedSeparator) {
			f := findFile(zr, part)
			if f == nil {
				return nil, out.Errorf("file-not-found", "%s holds no jar %s", s.Name, jar)
			}
			data, err := readEntry(f)
			if err != nil {
				return nil, err
			}
			if zr, err = openJar(jar, data); err != nil {
				return nil, err
			}
		}
	}
	path := classPath(name)
	f, where := findClass(zr, path, 0)
	if f == nil {
		return nil, out.Errorf("class-not-found", "%s holds no class %s", s.Name, strings.TrimSuffix(name, ".class"))
	}
	switch {
	case jar == "":
		jar = where
	case where != "":
		jar += nestedSeparator + where
	}
	data, err := readEntry(f)
	if err != nil {
		return nil, err
	}
	c, err := jarmeta.ReadClass(data)
	if err != nil {
		return nil, out.Errorf("class-invalid", "shulker can't read the class %s", path).WithCause("class", err)
	}
	return classReport(s.Name, jar, c), nil
}

// findClass finds the file at path in zr or, failing that, in the jars nested in it, and says which
// nested jar held it.
func findClass(zr *zip.Reader, path string, depth int) (*zip.File, string) {
	if f := findFile(zr, path); f != nil {
		return f, ""
	}
	for _, f := range zr.File {
		if !IsNestedJar(f.Name) {
			continue
		}
		inner, ok := openNested(f, depth)
		if !ok {
			continue
		}
		if found, where := findClass(inner, path, depth+1); found != nil {
			return found, strings.TrimSuffix(f.Name+nestedSeparator+where, nestedSeparator)
		}
	}
	return nil, ""
}

func classReport(name, jar string, c *jarmeta.Class) *ClassReport {
	rep := &ClassReport{
		Name:       name,
		Jar:        Untrusted(jar),
		Class:      Untrusted(jarmeta.JavaName(c.Name)),
		Super:      Untrusted(jarmeta.JavaName(c.Super)),
		Interfaces: []Untrusted{},
		Access:     modifiers(c.Access, classFlags),
		Fields:     []ClassField{},
		Methods:    []ClassMethod{},
	}
	for _, i := range c.Interfaces {
		rep.Interfaces = append(rep.Interfaces, Untrusted(jarmeta.JavaName(i)))
	}
	for _, f := range c.Fields {
		rep.Fields = append(rep.Fields, ClassField{Access: modifiers(f.Access, fieldFlags), Name: Untrusted(f.Name), Descriptor: Untrusted(f.Descriptor), Constant: Untrusted(f.Constant)})
	}
	for _, m := range c.Methods {
		cm := ClassMethod{Access: modifiers(m.Access, methodFlags), Name: Untrusted(m.Name), Descriptor: Untrusted(m.Descriptor), Calls: memberRefs(m.Calls), Fields: memberRefs(m.Fields), Strings: []Untrusted{}}
		for _, s := range m.Strings {
			cm.Strings = append(cm.Strings, Untrusted(s))
		}
		rep.Methods = append(rep.Methods, cm)
	}
	return rep
}

func memberRefs(refs []jarmeta.Ref) []MemberRef {
	found := []MemberRef{}
	for _, r := range refs {
		found = append(found, MemberRef{Op: r.Op, Owner: Untrusted(jarmeta.JavaName(r.Owner)), Name: Untrusted(r.Name), Descriptor: Untrusted(r.Descriptor)})
	}
	return found
}

// What a grep hit matched.
const (
	HitString   = "string"
	HitCall     = "call"
	HitField    = "field"
	HitConstant = "constant"
)

// GrepHit is one match: a string a method loads, a method it calls or a field it touches, or the
// string a constant field holds.
type GrepHit struct {
	Entry string `json:"entry"`
	// Jar is the nested jar the class is in, empty for the outermost.
	Jar   Untrusted `json:"jar,omitempty"`
	Class Untrusted `json:"class"`
	// Member is the method, or the constant field, the match is in.
	Member Untrusted `json:"member"`
	Kind   string    `json:"kind"`
	Text   Untrusted `json:"text"`
}

// Unreadable is a class grep couldn't read, so couldn't search. A class whose bytes can't hold a
// match isn't read, so it is never listed.
type Unreadable struct {
	Entry string    `json:"entry"`
	Jar   Untrusted `json:"jar,omitempty"`
	Path  Untrusted `json:"path"`
}

// GrepReport is every match across the jars searched.
type GrepReport struct {
	Pattern    string       `json:"pattern"`
	Jars       int          `json:"jars"`
	Hits       []GrepHit    `json:"hits"`
	Unreadable []Unreadable `json:"unreadable"`
	// Missing are the entries grep skipped because the cache doesn't hold their jar.
	Missing []string `json:"missing"`
}

// ProjectJars are the lock's mods whose jars the cache holds, with the keys of those it doesn't.
func ProjectJars(b *build.Builder) ([]Subject, []string) {
	var subjects []Subject
	missing := []string{}
	origins := entryOrigins(b)
	for _, key := range slices.Sorted(maps.Keys(b.Lock.Mods)) {
		m := b.Lock.Mods[key]
		if m.Sha512 == "" || !b.Cache.Has(m.Sha512) {
			missing = append(missing, key)
			continue
		}
		subjects = append(subjects, Subject{Name: key, Path: b.Cache.Object(m.Sha512), Origin: origins[key], Loader: loader.Running(b.Lock)})
	}
	return subjects, missing
}

// Grep searches the string constants and member refs of every class in each jar, nested jars
// included, for re. Jars are searched in parallel and the hits come back in the order given.
func Grep(ctx context.Context, subjects []Subject, re *regexp.Regexp) (*GrepReport, error) {
	results := make([]grepResult, len(subjects))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))
	needles := literalParts(re)
	for i, s := range subjects {
		g.Go(func() error {
			_, zr, err := readJar(s)
			if err != nil {
				return err
			}
			results[i].grepJar(ctx, zr, s.Name, "", re, needles, 0)
			return ctx.Err()
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	rep := &GrepReport{Pattern: re.String(), Jars: len(subjects), Hits: []GrepHit{}, Unreadable: []Unreadable{}, Missing: []string{}}
	for _, r := range results {
		rep.Hits = append(rep.Hits, r.hits...)
		rep.Unreadable = append(rep.Unreadable, r.unreadable...)
	}
	return rep, nil
}

type grepResult struct {
	hits       []GrepHit
	unreadable []Unreadable
}

func (r *grepResult) grepJar(ctx context.Context, zr *zip.Reader, entry, jar string, re *regexp.Regexp, needles []string, depth int) {
	for _, f := range zr.File {
		if ctx.Err() != nil {
			return
		}
		switch {
		case IsNestedJar(f.Name):
			if inner, ok := openNested(f, depth); ok {
				r.grepJar(ctx, inner, entry, strings.TrimPrefix(jar+nestedSeparator+f.Name, nestedSeparator), re, needles, depth+1)
			}
		case strings.HasSuffix(f.Name, ".class"):
			data, err := readEntry(f)
			if err == nil && !mentionsAll(data, needles) {
				continue
			}
			var c *jarmeta.Class
			if err == nil {
				c, err = jarmeta.ReadClass(data)
			}
			if err != nil {
				r.unreadable = append(r.unreadable, Unreadable{Entry: entry, Jar: Untrusted(jar), Path: Untrusted(f.Name)})
				continue
			}
			r.grepClass(c, entry, jar, re)
		}
	}
}

func (r *grepResult) grepClass(c *jarmeta.Class, entry, jar string, re *regexp.Regexp) {
	hit := func(member, kind, text string) {
		if re.MatchString(text) {
			r.hits = append(r.hits, GrepHit{Entry: entry, Jar: Untrusted(jar), Class: Untrusted(jarmeta.JavaName(c.Name)), Member: Untrusted(member), Kind: kind, Text: Untrusted(text)})
		}
	}
	for _, f := range c.Fields {
		if f.Constant != "" {
			hit(f.Name, HitConstant, f.Constant)
		}
	}
	for _, m := range c.Methods {
		member := m.Name + m.Descriptor
		for _, s := range m.Strings {
			hit(member, HitString, s)
		}
		for _, ref := range memberRefs(m.Calls) {
			hit(member, HitCall, refText(ref))
		}
		for _, ref := range memberRefs(m.Fields) {
			hit(member, HitField, refText(ref))
		}
	}
}

// refText writes a method ref as owner.name(descriptor) and a field ref as owner.name:descriptor,
// the way javap does.
func refText(m MemberRef) string {
	text := string(m.Name) + ":" + string(m.Descriptor)
	if strings.HasPrefix(string(m.Descriptor), "(") {
		text = string(m.Name) + string(m.Descriptor)
	}
	if m.Owner != "" {
		text = string(m.Owner) + "." + text
	}
	return text
}

// literalParts are the ASCII runs every match of re must contain, split where a class file stores
// the text apart: a ref's owner, name and descriptor sit in separate constants, and an owner's dots
// are slashes there. A class holding none of them can't match, so it isn't parsed.
func literalParts(re *regexp.Regexp) []string {
	prefix, _ := re.LiteralPrefix()
	return strings.FieldsFunc(prefix, func(r rune) bool {
		return r == '.' || r == '/' || r == '(' || r == ')' || r == ':' || r == 0 || r > 0x7f
	})
}

func mentionsAll(data []byte, needles []string) bool {
	for _, n := range needles {
		if !bytes.Contains(data, []byte(n)) {
			return false
		}
	}
	return true
}
