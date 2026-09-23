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
