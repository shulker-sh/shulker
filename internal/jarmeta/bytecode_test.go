package jarmeta

import (
	"os"
	"slices"
	"testing"
)

//go:generate javac --release 17 -d testdata testdata/Fixture.java

func fixtureClass(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/fixture/Fixture.class")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func method(t *testing.T, c *Class, name string) Method {
	t.Helper()
	for _, m := range c.Methods {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no method %s in %+v", name, c.Methods)
	return Method{}
}

func TestReadClassListsCallsFieldsAndStringsPerMethod(t *testing.T) {
	c, err := ReadClass(fixtureClass(t))
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "fixture/Fixture" || c.Super != "java/lang/Object" || !slices.Equal(c.Interfaces, []string{"java/lang/Runnable"}) {
		t.Fatalf("class: %+v", c)
	}
	if i := slices.IndexFunc(c.Fields, func(f Field) bool { return f.Name == "WEBHOOK" }); i < 0 || c.Fields[i].Constant != "https://discord.com/api/webhooks/1" {
		t.Fatalf("fields: %+v", c.Fields)
	}

	run := method(t, c, "run")
	if !slices.Contains(run.Fields, Ref{Op: "getfield", Owner: "fixture/Fixture", Name: "count", Descriptor: "I"}) ||
		!slices.Contains(run.Fields, Ref{Op: "putstatic", Owner: "fixture/Fixture", Name: "shared", Descriptor: "Ljava/lang/String;"}) ||
		!slices.Contains(run.Fields, Ref{Op: "getstatic", Owner: "java/lang/System", Name: "out", Descriptor: "Ljava/io/PrintStream;"}) {
		t.Fatalf("run fields: %+v", run.Fields)
	}
	if !slices.Contains(run.Calls, Ref{Op: "invokevirtual", Owner: "java/io/PrintStream", Name: "println", Descriptor: "(Ljava/lang/String;)V"}) || !slices.Equal(run.Strings, []string{"hello", "world"}) {
		t.Fatalf("run: %+v", run)
	}

	greet := method(t, c, "greet")
	if !slices.Contains(greet.Calls, Ref{Op: "invokeinterface", Owner: "java/util/function/Supplier", Name: "get", Descriptor: "()Ljava/lang/Object;"}) ||
		!slices.ContainsFunc(greet.Calls, func(r Ref) bool {
			return r.Op == "invokedynamic" && r.Owner == "" && r.Name == "makeConcatWithConstants"
		}) {
		t.Fatalf("greet calls: %+v", greet.Calls)
	}
	if lambda := method(t, c, "lambda$greet$0"); !slices.Equal(lambda.Strings, []string{"lambda"}) {
		t.Fatalf("lambda: %+v", lambda)
	}

	exec := method(t, c, "exec")
	if !slices.Contains(exec.Calls, Ref{Op: "invokevirtual", Owner: "java/lang/Runtime", Name: "exec", Descriptor: "(Ljava/lang/String;)Ljava/lang/Process;"}) || !slices.Equal(exec.Strings, []string{"calc.exe"}) {
		t.Fatalf("exec: %+v", exec)
	}
	if pick := method(t, c, "pick"); len(pick.Calls)+len(pick.Fields)+len(pick.Strings) != 0 {
		t.Fatalf("pick walks both switches and refers to nothing: %+v", pick)
	}
}

func TestReadClassFailsCleanlyOnMalformedInput(t *testing.T) {
	data := fixtureClass(t)
	for n := range len(data) {
		if _, err := ReadClass(data[:n]); err == nil {
			t.Fatalf("a class cut to %d bytes read without error", n)
		}
	}
	for i := range data {
		bad := slices.Clone(data)
		bad[i] ^= 0xff
		_, _ = ReadClass(bad)
	}
	for _, code := range [][]byte{
		{0xaa, 0, 0, 0, 0, 0, 0, 0, 0x7f, 0xff, 0xff, 0xff, 0x80, 0, 0, 0},
		{0xab, 0, 0, 0, 0, 0, 0, 0, 0x7f, 0xff, 0xff, 0xff},
		{0xab, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff},
		{0xca},
		{0xb6, 0},
		{0xc4},
	} {
		var m Method
		if err := m.readCode(code, nil); err == nil {
			t.Fatalf("% x read without error", code)
		}
	}
}

func TestInstructionLengthOfWide(t *testing.T) {
	for code, want := range map[string]int{"\xc4\x84\x01\x00\x00\x01": 6, "\xc4\x15\x01\x00": 4} {
		if n, err := instructionLength([]byte(code), 0); err != nil || n != want {
			t.Fatalf("% x: %d %v", code, n, err)
		}
	}
}
