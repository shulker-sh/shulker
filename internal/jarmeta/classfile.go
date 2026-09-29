package jarmeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
)

// fmlPackages are where FML's classes live: net.minecraftforge from 1.8, cpw before.
var fmlPackages = []string{"net/minecraftforge/fml/common/", "cpw/mods/fml/common/"}

var errClassTruncated = errors.New("class file truncated")

// classReader walks a class file's big-endian fields, remembering the first read past the end.
type classReader struct {
	data []byte
	pos  int
	err  error
}

func (r *classReader) take(n int) []byte {
	if r.err != nil || n < 0 || r.pos+n > len(r.data) {
		r.err = errClassTruncated
		return nil
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b
}

func (r *classReader) u1() int {
	if b := r.take(1); b != nil {
		return int(b[0])
	}
	return 0
}

func (r *classReader) u2() int {
	if b := r.take(2); b != nil {
		return int(binary.BigEndian.Uint16(b))
	}
	return 0
}

func (r *classReader) u4() int {
	if b := r.take(4); b != nil {
		return int(binary.BigEndian.Uint32(b))
	}
	return 0
}

// constant is a constant pool entry, holding what a mod declaration can refer to: UTF-8 text, ints,
// and the indexes a String, Class, Fieldref or NameAndType points through.
type constant struct {
	tag    int
	text   string
	number int
	ref    [2]int
}

type constantPool []constant

func (p constantPool) at(i int) constant {
	if i > 0 && i < len(p) {
		return p[i]
	}
	return constant{}
}

func (p constantPool) text(i int) string { return p.at(i).text }

// fmlClass reports whether a class name is FML's own class named simple.
func fmlClass(name, simple string) bool {
	for _, pkg := range fmlPackages {
		if name == pkg+simple {
			return true
		}
	}
	return false
}

// fmlDescriptor reports whether an annotation descriptor names FML's annotation simple.
func fmlDescriptor(descriptor, simple string) bool {
	return strings.HasPrefix(descriptor, "L") && strings.HasSuffix(descriptor, ";") && fmlClass(descriptor[1:len(descriptor)-1], simple)
}

// classDeclarations are the ways a class declares a mod to FML: the @Mod annotation's elements, a
// package's @API elements, and the modId and version a DummyModContainer subclass assigns its
// metadata. Only string and boolean annotation elements are kept.
type classDeclarations struct {
	mod       map[string]any
	api       map[string]any
	container map[string]string
}

// readClass reads what a class file declares to FML, skipping any class that names none of it.
func readClass(data []byte) (classDeclarations, error) {
	var d classDeclarations
	if !mentionsFML(data) {
		return d, nil
	}
	r := &classReader{data: data}
	if r.u4() != 0xCAFEBABE {
		return d, errors.New("not a class file")
	}
	r.take(4)
	pool, err := readPool(r)
	if err != nil {
		return d, err
	}
	r.take(4)
	super := pool.text(pool.at(r.u2()).ref[0])
	container := fmlClass(super, "DummyModContainer")
	r.take(2 * r.u2())
	for kind := range 2 {
		methods := kind == 1
		for range r.u2() {
			r.take(6)
			for range r.u2() {
				name, length := pool.text(r.u2()), r.u4()
				code := r.take(length)
				if methods && container && name == "Code" && len(code) >= 8 {
					if n := int(binary.BigEndian.Uint32(code[4:8])); 8+n <= len(code) {
						d.container = containerAssignments(code[8:8+n], pool, d.container)
					}
				}
			}
		}
	}
	for range r.u2() {
		name, length := pool.text(r.u2()), r.u4()
		if name != "RuntimeVisibleAnnotations" {
			r.take(length)
			continue
		}
		for range r.u2() {
			descriptor := pool.text(r.u2())
			values := map[string]any{}
			for range r.u2() {
				key := pool.text(r.u2())
				if v := elementValue(r, pool); v != nil {
					values[key] = v
				}
			}
			switch {
			case fmlDescriptor(descriptor, "Mod"):
				d.mod = values
			case fmlDescriptor(descriptor, "API"):
				d.api = values
			}
		}
	}
	return d, r.err
}

// readPool reads a class file's constant pool, keeping text, ints, and the indexes each reference
// points through.
func readPool(r *classReader) (constantPool, error) {
	pool := make(constantPool, r.u2())
	for i := 1; i < len(pool) && r.err == nil; i++ {
		pool[i].tag = r.u1()
		switch pool[i].tag {
		case 1:
			pool[i].text = string(r.take(r.u2()))
		case 3:
			pool[i].number = int(int32(r.u4()))
		case 4:
			r.take(4)
		case 5, 6:
			r.take(8)
			i++
		case 7, 8, 16, 19, 20:
			pool[i].ref[0] = r.u2()
		case 9, 10, 11, 12, 17, 18:
			pool[i].ref = [2]int{r.u2(), r.u2()}
		case 15:
			r.take(3)
		default:
			return nil, errors.New("unknown constant pool tag")
		}
	}
	return pool, r.err
}

func mentionsFML(data []byte) bool {
	for _, pkg := range fmlPackages {
		if bytes.Contains(data, []byte(pkg)) {
			return true
		}
	}
	return false
}

// containerAssignments finds a string constant loaded straight into ModMetadata's modId or version
// field, the way a DummyModContainer's constructor fills in its metadata: ldc or ldc_w, then
// putfield. Matching both constants rules out a false hit on an operand byte.
func containerAssignments(code []byte, pool constantPool, found map[string]string) map[string]string {
	for i := 0; i < len(code); i++ {
		var str, next int
		switch {
		case code[i] == 0x12 && i+2 < len(code):
			str, next = int(code[i+1]), i+2
		case code[i] == 0x13 && i+3 < len(code):
			str, next = int(binary.BigEndian.Uint16(code[i+1:])), i+3
		default:
			continue
		}
		if next+2 >= len(code) || code[next] != 0xb5 || pool.at(str).tag != 8 {
			continue
		}
		field := pool.at(int(binary.BigEndian.Uint16(code[next+1:])))
		if field.tag != 9 || !fmlClass(pool.text(pool.at(field.ref[0]).ref[0]), "ModMetadata") {
			continue
		}
		name := pool.text(pool.at(field.ref[1]).ref[0])
		if name != "modId" && name != "version" {
			continue
		}
		if found == nil {
			found = map[string]string{}
		}
		if _, seen := found[name]; !seen {
			found[name] = pool.text(pool.at(str).ref[0])
		}
	}
	return found
}

// elementValue reads one annotation element, returning a string or bool for the kinds @Mod's
// elements use and nil for the rest.
func elementValue(r *classReader, pool constantPool) any {
	switch tag := r.u1(); tag {
	case 's':
		return pool.text(r.u2())
	case 'Z':
		return pool.at(r.u2()).number != 0
	case 'B', 'C', 'D', 'F', 'I', 'J', 'S', 'c':
		r.take(2)
	case 'e':
		r.take(4)
	case '@':
		r.take(2)
		for range r.u2() {
			r.take(2)
			elementValue(r, pool)
		}
	case '[':
		for range r.u2() {
			elementValue(r, pool)
		}
	default:
		r.err = errors.New("unknown annotation element tag")
	}
	return nil
}
