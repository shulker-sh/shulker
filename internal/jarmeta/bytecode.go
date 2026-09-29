package jarmeta

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Class is one class file as javap -c would show it, reduced to what an audit reads: its members,
// and for each method what it calls, the fields it touches and the strings it loads.
type Class struct {
	Name       string
	Super      string
	Interfaces []string
	Access     int
	Fields     []Field
	Methods    []Method
}

// Field is a field a class declares. Constant is the string a static final field is initialised to
// by its ConstantValue attribute.
type Field struct {
	Access     int
	Name       string
	Descriptor string
	Constant   string
}

// Method is a method a class declares, with what its bytecode refers to in the order it appears.
type Method struct {
	Access     int
	Name       string
	Descriptor string
	Calls      []Ref
	Fields     []Ref
	Strings    []string
}

// Ref is a method or field an instruction names. Op is the instruction: invokevirtual,
// invokestatic, getfield, putstatic and so on. An invokedynamic has no Owner: its Name and
// Descriptor are the call site's, and its bootstrap method decides what it calls.
type Ref struct {
	Op         string
	Owner      string
	Name       string
	Descriptor string
}

// ReadClass reads a class file's members and the refs and strings in each method's bytecode.
// A malformed class fails with an error, never a panic.
func ReadClass(data []byte) (*Class, error) {
	r := &classReader{data: data}
	if r.u4() != 0xCAFEBABE {
		return nil, errors.New("not a class file")
	}
	r.take(4)
	pool, err := readPool(r)
	if err != nil {
		return nil, err
	}
	c := &Class{Access: r.u2()}
	c.Name = className(pool, r.u2())
	c.Super = className(pool, r.u2())
	for range r.u2() {
		c.Interfaces = append(c.Interfaces, className(pool, r.u2()))
	}
	for range r.u2() {
		f := Field{Access: r.u2(), Name: pool.text(r.u2()), Descriptor: pool.text(r.u2())}
		for range r.u2() {
			name, body := pool.text(r.u2()), r.take(r.u4())
			if name == "ConstantValue" && len(body) == 2 {
				if v := pool.at(int(binary.BigEndian.Uint16(body))); v.tag == 8 {
					f.Constant = pool.text(v.ref[0])
				}
			}
		}
		c.Fields = append(c.Fields, f)
	}
	for range r.u2() {
		m := Method{Access: r.u2(), Name: pool.text(r.u2()), Descriptor: pool.text(r.u2())}
		for range r.u2() {
			name, body := pool.text(r.u2()), r.take(r.u4())
			if name != "Code" || r.err != nil {
				continue
			}
			if len(body) < 8 {
				return nil, errClassTruncated
			}
			n := int(binary.BigEndian.Uint32(body[4:8]))
			if n > len(body)-8 {
				return nil, errClassTruncated
			}
			if err := m.readCode(body[8:8+n], pool); err != nil {
				return nil, fmt.Errorf("%s%s: %w", m.Name, m.Descriptor, err)
			}
		}
		c.Methods = append(c.Methods, m)
	}
	for range r.u2() {
		r.take(2)
		r.take(r.u4())
	}
	if r.err != nil {
		return nil, r.err
	}
	return c, nil
}

func className(pool constantPool, i int) string {
	if c := pool.at(i); c.tag == 7 {
		return pool.text(c.ref[0])
	}
	return ""
}

var (
	errBadOpcode = errors.New("unknown opcode")
	invokeOps    = map[byte]string{0xb6: "invokevirtual", 0xb7: "invokespecial", 0xb8: "invokestatic", 0xb9: "invokeinterface", 0xba: "invokedynamic"}
	fieldOps     = map[byte]string{0xb2: "getstatic", 0xb3: "putstatic", 0xb4: "getfield", 0xb5: "putfield"}
)

// readCode walks one method's instructions, keeping each invoke, field access and string ldc.
func (m *Method) readCode(code []byte, pool constantPool) error {
	for i := 0; i < len(code); {
		op := code[i]
		n, err := instructionLength(code, i)
		if err != nil {
			return err
		}
		if i+n > len(code) {
			return errClassTruncated
		}
		switch {
		case op == 0x12:
			m.addString(pool, int(code[i+1]))
		case op == 0x13:
			m.addString(pool, int(binary.BigEndian.Uint16(code[i+1:])))
		case invokeOps[op] != "":
			m.Calls = append(m.Calls, memberRef(pool, invokeOps[op], int(binary.BigEndian.Uint16(code[i+1:]))))
		case fieldOps[op] != "":
			m.Fields = append(m.Fields, memberRef(pool, fieldOps[op], int(binary.BigEndian.Uint16(code[i+1:]))))
		}
		i += n
	}
	return nil
}

func (m *Method) addString(pool constantPool, i int) {
	if c := pool.at(i); c.tag == 8 {
		m.Strings = append(m.Strings, pool.text(c.ref[0]))
	}
}

// memberRef follows a Fieldref, Methodref, InterfaceMethodref or InvokeDynamic to the names it
// holds.
func memberRef(pool constantPool, op string, i int) Ref {
	c := pool.at(i)
	nameAndType := pool.at(c.ref[1])
	ref := Ref{Op: op, Name: pool.text(nameAndType.ref[0]), Descriptor: pool.text(nameAndType.ref[1])}
	if c.tag != 18 {
		ref.Owner = className(pool, c.ref[0])
	}
	return ref
}

// instructionLength is how many bytes the instruction at i takes, operands included.
func instructionLength(code []byte, i int) (int, error) {
	op := code[i]
	switch {
	case op <= 0x0f, op >= 0x1a && op <= 0x35, op >= 0x3b && op <= 0x83, op >= 0x85 && op <= 0x98, op >= 0xac && op <= 0xb1, op == 0xbe, op == 0xbf, op == 0xc2, op == 0xc3:
		return 1, nil
	case op == 0x10, op == 0x12, op >= 0x15 && op <= 0x19, op >= 0x36 && op <= 0x3a, op == 0xa9, op == 0xbc:
		return 2, nil
	case op == 0x11, op == 0x13, op == 0x14, op == 0x84, op >= 0x99 && op <= 0xa8, op >= 0xb2 && op <= 0xb8, op == 0xbb, op == 0xbd, op == 0xc0, op == 0xc1, op == 0xc6, op == 0xc7:
		return 3, nil
	case op == 0xc5:
		return 4, nil
	case op == 0xb9, op == 0xba, op == 0xc8, op == 0xc9:
		return 5, nil
	case op == 0xc4:
		if i+1 < len(code) && code[i+1] == 0x84 {
			return 6, nil
		}
		return 4, nil
	case op == 0xaa, op == 0xab:
		return switchLength(code, i)
	}
	return 0, fmt.Errorf("%w 0x%02x", errBadOpcode, op)
}

// switchLength measures a tableswitch or lookupswitch, whose operands start at the next multiple
// of four from the start of the code.
func switchLength(code []byte, i int) (int, error) {
	start := i + 1 + (4-(i+1)%4)%4
	word := func(at int) (int64, bool) {
		if at < 0 || at+4 > len(code) {
			return 0, false
		}
		return int64(int32(binary.BigEndian.Uint32(code[at:]))), true
	}
	var body int64
	if code[i] == 0xaa {
		low, ok1 := word(start + 4)
		high, ok2 := word(start + 8)
		if !ok1 || !ok2 || high < low {
			return 0, errClassTruncated
		}
		body = 12 + 4*(high-low+1)
	} else {
		pairs, ok := word(start + 4)
		if !ok || pairs < 0 {
			return 0, errClassTruncated
		}
		body = 8 + 8*pairs
	}
	if end := int64(start) + body; end > int64(len(code)) {
		return 0, errClassTruncated
	}
	return start - i + int(body), nil
}

// JavaName is a binary class name as Java source writes it: java/lang/String becomes
// java.lang.String.
func JavaName(internal string) string { return strings.ReplaceAll(internal, "/", ".") }
