package manifest

import (
	"encoding/json"
	"testing"
)

func TestIDKeepsItsToken(t *testing.T) {
	for _, raw := range []string{`238222`, `"AANobbMI"`, `"12345678"`} {
		var id ID
		if err := json.Unmarshal([]byte(raw), &id); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(id)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != raw {
			t.Errorf("%s came back as %s", raw, got)
		}
	}
}

func TestNewIDWritesCurseForgeAsInteger(t *testing.T) {
	for _, c := range []struct{ provider, id, want string }{
		{"curseforge", "238222", `238222`},
		{"modrinth", "12345678", `"12345678"`},
		{"modrinth", "AANobbMI", `"AANobbMI"`},
	} {
		got, err := json.Marshal(NewID(c.provider, c.id))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("%s %s: got %s, want %s", c.provider, c.id, got, c.want)
		}
	}
}

func TestZeroIDIsOmitted(t *testing.T) {
	got, err := json.Marshal(Require{Type: TypeMod})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"type":"mod"}` {
		t.Errorf("got %s", got)
	}
}

func TestAStringCurseForgeIDEqualsItsInteger(t *testing.T) {
	var typed ID
	if err := json.Unmarshal([]byte(`"12345678"`), &typed); err != nil {
		t.Fatal(err)
	}
	if typed.For("curseforge") != NewID("curseforge", "12345678") {
		t.Error("a CurseForge id typed as a string differs from the integer")
	}
	if typed.For("modrinth") != typed {
		t.Error("a Modrinth id changed")
	}
}
