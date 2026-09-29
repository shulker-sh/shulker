package audit

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func fixtureClass(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../jarmeta/testdata/fixture/Fixture.class")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func classJar(t *testing.T) []byte {
	t.Helper()
	inner := zipOf(t, text("fabric.mod.json", `{"id":"lib"}`), zipFile{"fixture/Fixture.class", fixtureClass(t)})
	return zipOf(t,
		text("fabric.mod.json", `{"id":"outer"}`),
		zipFile{"META-INF/jars/lib.jar", inner},
		text("broken/Bad.class", "\xca\xfe\xba\xbe\x00"),
	)
}

func TestInspectClassFindsAClassInANestedJar(t *testing.T) {
	s := jarSubject(t, classJar(t))
	for _, name := range []string{"fixture.Fixture", "fixture/Fixture.class", "META-INF/jars/lib.jar!/fixture.Fixture"} {
		rep, err := InspectClass(s, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if rep.Jar != "META-INF/jars/lib.jar" || rep.Class != "fixture.Fixture" || !slices.Equal(rep.Interfaces, []Untrusted{"java.lang.Runnable"}) || !slices.Equal(rep.Access, []string{"public"}) {
			t.Fatalf("%s: %+v", name, rep)
		}
		i := slices.IndexFunc(rep.Methods, func(m ClassMethod) bool { return m.Name == "exec" })
		if i < 0 || !slices.Contains(rep.Methods[i].Calls, MemberRef{Op: "invokevirtual", Owner: "java.lang.Runtime", Name: "exec", Descriptor: "(Ljava/lang/String;)Ljava/lang/Process;"}) {
			t.Fatalf("%s: exec calls %+v", name, rep.Methods)
		}
	}
	for name, code := range map[string]string{"fixture.Missing": "class-not-found", "broken.Bad": "class-invalid", "nope.jar!/fixture.Fixture": "file-not-found"} {
		_, err := InspectClass(s, name)
		var e *out.Error
		if !errors.As(err, &e) || e.Code != code {
			t.Fatalf("%s: got %v, want %s", name, err, code)
		}
	}
}

func TestGrepFindsStringsAndRefsInNestedJars(t *testing.T) {
	s := jarSubject(t, classJar(t))
	for pattern, want := range map[string]GrepHit{
		`discord\.com/api/webhooks`: {Entry: "mod.jar", Jar: "META-INF/jars/lib.jar", Class: "fixture.Fixture", Member: "WEBHOOK", Kind: HitConstant, Text: "https://discord.com/api/webhooks/1"},
		`calc\.exe`:                 {Entry: "mod.jar", Jar: "META-INF/jars/lib.jar", Class: "fixture.Fixture", Member: "exec()V", Kind: HitString, Text: "calc.exe"},
		`Runtime.exec`:              {Entry: "mod.jar", Jar: "META-INF/jars/lib.jar", Class: "fixture.Fixture", Member: "exec()V", Kind: HitCall, Text: "java.lang.Runtime.exec(Ljava/lang/String;)Ljava/lang/Process;"},
		`(?i)SYSTEM\.OUT`:           {Entry: "mod.jar", Jar: "META-INF/jars/lib.jar", Class: "fixture.Fixture", Member: "run()V", Kind: HitField, Text: "java.lang.System.out:Ljava/io/PrintStream;"},
	} {
		rep, err := Grep(context.Background(), []Subject{s}, regexp.MustCompile(pattern))
		if err != nil {
			t.Fatal(err)
		}
		if len(rep.Hits) != 1 || rep.Hits[0] != want {
			t.Fatalf("%s: %+v", pattern, rep.Hits)
		}
	}
	rep, err := Grep(context.Background(), []Subject{s}, regexp.MustCompile(`.`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Unreadable) != 1 || rep.Unreadable[0].Path != "broken/Bad.class" || rep.Unreadable[0].Jar != "" {
		t.Fatalf("unreadable %+v", rep.Unreadable)
	}
}

func TestLiteralPartsSplitWhereAClassStoresTextApart(t *testing.T) {
	for pattern, want := range map[string][]string{
		`java\.lang\.Runtime\.exec\(`: {"java", "lang", "Runtime", "exec"},
		`(?i)runtime`:                 nil,
		`calc.exe`:                    {"calc"},
	} {
		if got := literalParts(regexp.MustCompile(pattern)); !slices.Equal(got, want) {
			t.Fatalf("%s: %q", pattern, got)
		}
	}
}
