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

func TestHostedModpackEntries(t *testing.T) {
	sha := strings.Repeat("ab", 64)
	doc := func(modpack string) []byte {
		return []byte(`{"lockVersion":1,"minecraft":"26.2","loader":{"type":"fabric","version":"0.17.3"},"java":{"major":25,"component":"java-runtime-epsilon"},` +
			`"modpacks":{"cozy":{` + modpack + `}},"mods":{},"resourcepacks":{},"shaders":{},"players":[]}`)
	}
	identity := `"provider":"curseforge","project":600001,"version":7000001,"versionNumber":"Cozy 2.0","channel":"release","filename":"cozy-2.0.zip","sha512":"` + sha + `","size":10,"locked":true`
	hosted := identity + `,"url":"https://edge.forgecdn.net/files/cozy-2.0.zip"`
	l, err := Parse(doc(hosted))
	if err != nil {
		t.Fatal(err)
	}
	if data, err := l.Encode(); err != nil || !strings.Contains(string(data), `"url": "https://edge.forgecdn.net/files/cozy-2.0.zip"`) {
		t.Fatalf("a hosted entry writes its url: %v\n%s", err, data)
	}
	mp := l.Modpacks["cozy"]
	if mp.Provider != "curseforge" || mp.VersionNumber != "Cozy 2.0" || mp.URL == nil || mp.Sha512 != sha || mp.Label() != "Cozy 2.0" {
		t.Fatalf("hosted entry: %+v", mp)
	}
	manual, err := Parse(doc(identity + `,"url":null,"page":"https://www.curseforge.com/minecraft/modpacks/cozy/files/7000001"`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := manual.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"url": null`) {
		t.Fatalf("a hosted entry writes a null url:\n%s", data)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("a hosted entry must survive a round trip: %v\n%s", err, data)
	}
	for name, entry := range map[string]string{
		"a hosted pack with a source":       hosted + `,"source":"../base"`,
		"a hosted pack with a file":         hosted + `,"file":"packs/cozy.zip"`,
		"a hosted pack without its version": `"provider":"modrinth","project":"AAAAAAAA","filename":"cozy.mrpack","url":"https://cdn.modrinth.com/cozy.mrpack","sha512":"` + sha + `","size":10`,
		"a hosted pack without a url":       identity,
		"a null url with no page":           identity + `,"url":null`,
		"a source with a version":           `"source":"../base","dirSha256":"` + strings.Repeat("ab", 32) + `","version":"AAAAAAAA"`,
	} {
		if _, err := Parse(doc(entry)); err == nil {
			t.Errorf("%s should be invalid", name)
		}
	}
}
