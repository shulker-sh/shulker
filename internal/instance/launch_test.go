package instance

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestLaunchKeysNameEveryLaunchSetting(t *testing.T) {
	var tags []string
	typ := reflect.TypeFor[LaunchSettings]()
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		tags = append(tags, name)
	}
	if !slices.Equal(tags, LaunchKeys) {
		t.Fatalf("LaunchKeys %v, fields %v", LaunchKeys, tags)
	}
}

func TestOverFillsOnlyUnsetSettings(t *testing.T) {
	own := LaunchSettings{Memory: "4G", Wrapper: []string{}}
	base := LaunchSettings{Memory: "2G", JVMArgs: []string{"-X"}, Java: "/java", Window: "800x600", Wrapper: []string{"gamemoderun"}}
	got := own.Over(base)
	want := LaunchSettings{Memory: "4G", JVMArgs: []string{"-X"}, Java: "/java", Window: "800x600", Wrapper: []string{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestForLaunchTakesMemoryFromTheInstanceThenConfigThenPackThenDefault(t *testing.T) {
	base := LaunchSettings{Memory: "2G", Java: "/java"}
	cases := []struct {
		own  LaunchSettings
		base LaunchSettings
		pack string
		want string
	}{
		{LaunchSettings{Memory: "8G"}, base, "6G", "8G"},
		{LaunchSettings{}, base, "6G", "2G"},
		{LaunchSettings{}, LaunchSettings{}, "6G", "6G"},
		{LaunchSettings{}, LaunchSettings{}, "", DefaultMemory},
	}
	for _, c := range cases {
		got := c.own.ForLaunch(c.base, c.pack)
		if got.Memory != c.want {
			t.Errorf("ForLaunch(%+v, %+v, %q).Memory = %q, want %q", c.own, c.base, c.pack, got.Memory, c.want)
		}
		if got.Java != c.base.Java {
			t.Errorf("ForLaunch dropped the config default for java: %+v", got)
		}
	}
	if DefaultMemory != "4G" {
		t.Fatalf("DefaultMemory = %q, want the ordinary launcher default", DefaultMemory)
	}
}
