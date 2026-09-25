package jarmeta

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"maps"
	"slices"
	"testing"
)

// classFile builds a minimal class file: a pool that grows as entries are asked for, one
// class-level annotation, and optionally one method body.
type classFile struct {
	pool  bytes.Buffer
	count int
}

func (c *classFile) entry(tag byte, fields ...any) int {
	c.pool.WriteByte(tag)
	for _, f := range fields {
		binary.Write(&c.pool, binary.BigEndian, f)
	}
	c.count++
	return c.count
}

func (c *classFile) utf8(s string) int {
	c.pool.WriteByte(1)
	binary.Write(&c.pool, binary.BigEndian, uint16(len(s)))
	c.pool.WriteString(s)
	c.count++
	return c.count
}

func (c *classFile) class(name string) int { return c.entry(7, uint16(c.utf8(name))) }

// bytes assembles the class with super as its superclass, the annotation (descriptor empty for
// none) and a method whose Code is code.
func (c *classFile) bytes(t *testing.T, super, descriptor string, elements map[string]any, code []byte) string {
	t.Helper()
	this, sup := c.class("example/Mod"), c.class(super)
	var attrs bytes.Buffer
	attrCount := 0
	if descriptor != "" {
		name, desc := c.utf8("RuntimeVisibleAnnotations"), c.utf8(descriptor)
		var a bytes.Buffer
		binary.Write(&a, binary.BigEndian, []uint16{1, uint16(desc), uint16(len(elements))})
		for _, key := range slices.Sorted(maps.Keys(elements)) {
			binary.Write(&a, binary.BigEndian, uint16(c.utf8(key)))
			switch v := elements[key].(type) {
			case string:
				a.WriteByte('s')
				binary.Write(&a, binary.BigEndian, uint16(c.utf8(v)))
			case bool:
				n := int32(0)
				if v {
					n = 1
				}
				a.WriteByte('Z')
				binary.Write(&a, binary.BigEndian, uint16(c.entry(3, n)))
			default:
				t.Fatalf("element %s: unsupported %T", key, v)
			}
		}
		binary.Write(&attrs, binary.BigEndian, uint16(name))
		binary.Write(&attrs, binary.BigEndian, uint32(a.Len()))
		attrs.Write(a.Bytes())
		attrCount++
	}
	var methods bytes.Buffer
	methodCount := 0
	if code != nil {
		name, desc, codeName := c.utf8("<init>"), c.utf8("()V"), c.utf8("Code")
		var body bytes.Buffer
		binary.Write(&body, binary.BigEndian, []uint16{2, 2})
		binary.Write(&body, binary.BigEndian, uint32(len(code)))
		body.Write(code)
		binary.Write(&body, binary.BigEndian, []uint16{0, 0})
		binary.Write(&methods, binary.BigEndian, []uint16{1, uint16(name), uint16(desc), 1, uint16(codeName)})
		binary.Write(&methods, binary.BigEndian, uint32(body.Len()))
		methods.Write(body.Bytes())
		methodCount++
	}
	var out bytes.Buffer
	binary.Write(&out, binary.BigEndian, uint32(0xCAFEBABE))
	binary.Write(&out, binary.BigEndian, []uint16{0, 52, uint16(c.count + 1)})
	out.Write(c.pool.Bytes())
	binary.Write(&out, binary.BigEndian, []uint16{0x21, uint16(this), uint16(sup), 0, 0, uint16(methodCount)})
	out.Write(methods.Bytes())
	binary.Write(&out, binary.BigEndian, uint16(attrCount))
	out.Write(attrs.Bytes())
	return out.String()
}

// modClass builds a class file carrying the annotation descriptor with the given elements, each a
// string or a bool.
func modClass(t *testing.T, descriptor string, elements map[string]any) string {
	return (&classFile{}).bytes(t, "java/lang/Object", descriptor, elements, nil)
}

// containerClass builds a DummyModContainer whose constructor sets its metadata's modId and version.
func containerClass(t *testing.T, modID, version string) string {
	c := &classFile{}
	meta := c.class("net/minecraftforge/fml/common/ModMetadata")
	field := func(name string) int {
		nat := c.entry(12, uint16(c.utf8(name)), uint16(c.utf8("Ljava/lang/String;")))
		return c.entry(9, uint16(meta), uint16(nat))
	}
	idField, versionField := field("modId"), field("version")
	idString, versionString := c.entry(8, uint16(c.utf8(modID))), c.entry(8, uint16(c.utf8(version)))
	code := []byte{0x2b, 0x12, byte(idString), 0xb5, 0, byte(idField), 0x2b, 0x13, 0, byte(versionString), 0xb5, 0, byte(versionField), 0xb1}
	return c.bytes(t, "net/minecraftforge/fml/common/DummyModContainer", "", nil, code)
}

func readLegacy(t *testing.T, files map[string]string) *Info {
	t.Helper()
	data := buildZip(t, files)
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return readLegacyForge(zr)
}

func TestReadLegacyForgeFromModAnnotation(t *testing.T) {
	info := readLegacy(t, map[string]string{
		"ladysnake/spawnercontrol/SpawnerControl.class": modClass(t, "Lnet/minecraftforge/fml/common/Mod;", map[string]any{
			"modid":        "spawnercontrol",
			"version":      "1.6.3b",
			"dependencies": "required-after:Forge@[14.23.2.2596,);after:jei@[4.8,);required-client:ctm;required-after:FML",
		}),
	})
	if info.ID != "spawnercontrol" || info.Version != "1.6.3b" || info.Loader != "forge" || info.Side != "both" || !info.UsesMavenRanges {
		t.Fatalf("got %+v", info)
	}
	if want := map[string]Range{"forge": {"[14.23.2.2596,)"}}; !maps.EqualFunc(info.Depends, want, slices.Equal) {
		t.Errorf("depends %v", info.Depends)
	}
	if want := map[string]Range{"jei": {"[4.8,)"}, "ctm": {"*"}}; !maps.EqualFunc(info.Optional, want, slices.Equal) {
		t.Errorf("optional %v", info.Optional)
	}
}

func TestReadLegacyForgeFillsVersionFromMcmodInfo(t *testing.T) {
	info := readLegacy(t, map[string]string{
		"mcmod.info":    "[{\"modid\": \"kiwi\", \"version\": \"0.5.3.32\", \"description\": \"line\u0001break\", \"dependencies\": [mod_minecraftForge]}]",
		"a/Other.class": modClass(t, "Lnet/minecraftforge/fml/common/Mod;", map[string]any{"modid": "kiwiaddon", "version": "2", "clientSideOnly": true}),
		"b/Kiwi.class":  modClass(t, "Lnet/minecraftforge/fml/common/Mod;", map[string]any{"modid": "kiwi", "clientSideOnly": true}),
	})
	if info.ID != "kiwi" || info.Version != "0.5.3.32" || info.Side != "client" {
		t.Fatalf("got %+v", info)
	}
	if want := map[string]string{"kiwiaddon": "2"}; !maps.Equal(info.Provides, want) {
		t.Errorf("provides %v", info.Provides)
	}
}

func TestReadLegacyForgeUsesMcmodDependenciesWhenAsked(t *testing.T) {
	info := readLegacy(t, map[string]string{
		"mcmod.info": `{"modListVersion": 2, "modList": [{"modid": "a", "version": "1", "useDependencyInformation": true, "requiredMods": ["b@[2,)"], "dependencies": ["c"]}]}`,
		"A.class":    modClass(t, "Lcpw/mods/fml/common/Mod;", map[string]any{"modid": "a", "dependencies": "required-after:ignored"}),
	})
	if !maps.EqualFunc(info.Depends, map[string]Range{"b": {"[2,)"}}, slices.Equal) || !maps.EqualFunc(info.Optional, map[string]Range{"c": {"*"}}, slices.Equal) {
		t.Errorf("depends %v, optional %v", info.Depends, info.Optional)
	}
}

func TestReadLegacyForgeMcmodInfoAlone(t *testing.T) {
	info := readLegacy(t, map[string]string{"mcmod.info": `[{"modid": "only", "version": "3"}]`})
	if info.ID != "only" || info.Version != "3" {
		t.Errorf("got %+v", info)
	}
}

func TestReadLegacyForgeIgnoresUnreadableMcmodInfo(t *testing.T) {
	info := readLegacy(t, map[string]string{
		"mcmod.info": `[{"modid": "broken", "version": "1"},] // trailing`,
		"A.class":    modClass(t, "Lnet/minecraftforge/fml/common/Mod;", map[string]any{"modid": "fromclass", "version": "2"}),
	})
	if info.ID != "fromclass" || info.Version != "2" {
		t.Errorf("got %+v", info)
	}
}

func TestReadLegacyForgeCoremodHasNoID(t *testing.T) {
	info := readLegacy(t, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nFMLCorePlugin: x.Core\n", "x/Core.class": "not a mod"})
	if info.ID != "" || info.Loader != "forge" {
		t.Errorf("got %+v", info)
	}
}

func TestReadLegacyForgeCoremodContainerAndAPI(t *testing.T) {
	info := readLegacy(t, map[string]string{
		"mcmod.info":                                   `[{"modid": "tnt_utilities", "version": "1.2.3"}]`,
		"ljfa/tntutils/TNTUtils.class":                 modClass(t, "Lnet/minecraftforge/fml/common/Mod;", map[string]any{"modid": "tnt_utilities", "dependencies": "required-after:tnt_utilities_core;required:rtgapi@[1.0.0,)"}),
		"ljfa/tntutils/asm/TntuCoremodContainer.class": containerClass(t, "tnt_utilities_core", "1.2.3"),
		"rtg/api/package-info.class":                   modClass(t, "Lnet/minecraftforge/fml/common/API;", map[string]any{"owner": "tnt_utilities", "provides": "rtgapi", "apiVersion": "1.0.0"}),
	})
	if info.ID != "tnt_utilities" || info.Version != "1.2.3" {
		t.Fatalf("got %+v", info)
	}
	if want := map[string]string{"tnt_utilities_core": "1.2.3", "rtgapi": "1.0.0"}; !maps.Equal(info.Provides, want) {
		t.Errorf("provides %v", info.Provides)
	}
	if len(info.Depends) != 0 {
		t.Errorf("depends on its own ids: %v", info.Depends)
	}
}

func TestReadLegacyForgeCoremodContainerAlone(t *testing.T) {
	info := readLegacy(t, map[string]string{
		"mcmod.info":             `[{"modid": "IvToolkit", "version": "1.3.3-1.12"}]`,
		"iv/CoreContainer.class": containerClass(t, "ivtoolkit", "1.3.3"),
	})
	if info.ID != "ivtoolkit" || info.Version != "1.3.3" {
		t.Errorf("got %+v", info)
	}
}
