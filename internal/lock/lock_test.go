package lock

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	l, err := Load(filepath.Join("..", "..", "testdata", "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := l.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if again.Mods["betterthirdperson"].URL != nil || again.Mods["betterthirdperson"].Page == "" {
		t.Fatal("null url and page must survive a round trip")
	}
	if again.Mods["appleskin"].Aliases.CurseForge != 248787 {
		t.Fatal("aliases must survive a round trip")
	}
}

func TestNewSaves(t *testing.T) {
	l := New()
	l.Minecraft = "26.2"
	l.Loader = Loader{Type: "fabric", Version: "0.17.3"}
	l.Java = Java{Major: 25, Component: "java-runtime-epsilon"}
	if err := l.Save(filepath.Join(t.TempDir(), FileName)); err != nil {
		t.Fatal(err)
	}
}

func TestFileEntries(t *testing.T) {
	sha := strings.Repeat("ab", 64)
	doc := func(mod, pack string) []byte {
		return []byte(`{"lockVersion":1,"minecraft":"26.2","loader":{"type":"fabric","version":"0.17.3"},"java":{"major":25,"component":"java-runtime-epsilon"},"modpacks":{},` +
			`"mods":{"extras":{` + mod + `}},"resourcepacks":{"faithful":{` + pack + `}},"shaders":{},"players":[]}`)
	}
	mod := `"file":"files/extras.jar","filename":"extras.jar","sha512":"` + sha + `","size":10,"side":"both","requiredBy":[],"aliases":{}`
	pack := `"file":"files/faithful.zip","filename":"faithful.zip","sha512":"` + sha + `","size":10`
	l, err := Parse(doc(mod, pack))
	if err != nil {
		t.Fatal(err)
	}
	if l.Mods["extras"].File != "files/extras.jar" || l.ResourcePacks["faithful"].File != "files/faithful.zip" {
		t.Fatalf("file entries: %+v %+v", l.Mods, l.ResourcePacks)
	}
	data, err := l.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("a file entry must survive a round trip: %v\n%s", err, data)
	}
	for _, key := range []string{`"provider":"modrinth"`, `"project":"AANobbMI"`, `"version":"AANobbMI"`, `"versionNumber":"1.0"`, `"url":null`, `"page":"https://example.com"`, `"channel":"release"`} {
		if _, err := Parse(doc(mod+","+key, pack)); err == nil {
			t.Errorf("mod: %s beside file should be invalid", key)
		}
		if _, err := Parse(doc(mod, pack+","+key)); err == nil {
			t.Errorf("pack: %s beside file should be invalid", key)
		}
	}
	if _, err := Parse(doc(`"file":"files/extras.jar","filename":"extras.jar","size":10,"side":"both","requiredBy":[],"aliases":{}`, pack)); err == nil {
		t.Error("a file entry needs its sha512")
	}
}

func TestArchiveModpackEntries(t *testing.T) {
	sha := strings.Repeat("ab", 64)
	doc := func(modpack string) []byte {
		return []byte(`{"lockVersion":1,"minecraft":"26.2","loader":{"type":"fabric","version":"0.17.3"},"java":{"major":25,"component":"java-runtime-epsilon"},` +
			`"modpacks":{"cozy":{` + modpack + `}},"mods":{},"resourcepacks":{},"shaders":{},"players":[]}`)
	}
	archive := `"file":"packs/cozy.mrpack","sha512":"` + sha + `","size":10,"locked":true,"unmanaged":{"server-overrides/mods/extra.jar":"` + sha + `"}`
	l, err := Parse(doc(archive))
	if err != nil {
		t.Fatal(err)
	}
	if mp := l.Modpacks["cozy"]; mp.File != "packs/cozy.mrpack" || mp.Sha512 != sha || mp.Size != 10 || !mp.UsesLock || mp.Unmanaged["server-overrides/mods/extra.jar"] != sha {
		t.Fatalf("archive entry: %+v", mp)
	}
	data, err := l.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("an archive entry must survive a round trip: %v\n%s", err, data)
	}
	source := `"source":"../base","dirSha256":"` + strings.Repeat("ab", 32) + `"`
	for name, entry := range map[string]string{
		"an archive with a source":         archive + `,"source":"../base"`,
		"an archive with a lock hash":      archive + `,"lockSha256":"` + strings.Repeat("ab", 32) + `"`,
		"an archive without its size":      `"file":"packs/cozy.mrpack","sha512":"` + sha + `"`,
		"a source with a sha512":           source + `,"sha512":"` + sha + `"`,
		"a locked source with no lockhash": source + `,"locked":true`,
		"an unmanaged path outside layers": `"file":"packs/cozy.mrpack","sha512":"` + sha + `","size":10,"unmanaged":{"mods/extra.jar":"` + sha + `"}`,
	} {
		if _, err := Parse(doc(entry)); err == nil {
			t.Errorf("%s should be invalid", name)
		}
	}
}
