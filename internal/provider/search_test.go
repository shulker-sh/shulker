package provider

import "testing"

func hit(provider, id, slug, title, author string, downloads int64) Hit {
	return Hit{Provider: provider, Project: Project{ID: id, Slug: slug, Title: title, Type: "mod", Author: author, Downloads: downloads}}
}

func TestMergePairsTheSameListing(t *testing.T) {
	hits := []Hit{
		hit("modrinth", "AANobbMI", "sodium", "Sodium", "jellysquid3", 200),
		hit("modrinth", "P7dR8mSH", "fabric-api", "Fabric API", "modmuss50", 900),
		hit("modrinth", "aaa", "clash", "Clash", "one", 5),
		hit("curseforge", "394468", "sodium", "Sodium [Fabric] - DISCONTINUED", "someone", 100),
		hit("curseforge", "306612", "fabric-api", "Fabric API Renamed", "Modmuss50", 50),
		hit("curseforge", "999", "clash", "Different Mod", "two", 1),
	}
	results := Merge(hits, nil)
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4: %+v", len(results), results)
	}
	if r := results[0]; r.Slug() != "fabric-api" || len(r.Hits) != 2 || r.Downloads() != 950 {
		t.Errorf("merged by author: %+v", r)
	}
	if r := results[1]; r.Slug() != "sodium" || len(r.Hits) != 2 || r.Downloads() != 300 || r.Title() != "Sodium" {
		t.Errorf("merged by decorated name: %+v", r)
	}
	if results[2].Slug() != "clash" || results[3].Slug() != "clash" {
		t.Errorf("same slug, different name and author, stays apart: %+v", results[2:])
	}
}

func TestMergeTakesAPairing(t *testing.T) {
	hits := []Hit{
		hit("modrinth", "mr", "jei", "Just Enough Items", "mezz", 10),
		hit("curseforge", "238222", "jei-cf", "JEI", "other", 20),
	}
	results := Merge(hits, func(a, b Hit) bool { return a.ID == "mr" && b.ID == "238222" })
	if len(results) != 1 || results[0].Downloads() != 30 {
		t.Errorf("paired hits: %+v", results)
	}
}

func TestMergeKeepsTypesApart(t *testing.T) {
	pack := hit("curseforge", "1", "sodium", "Sodium", "jellysquid3", 1)
	pack.Type = "modpack"
	results := Merge([]Hit{hit("modrinth", "AANobbMI", "sodium", "Sodium", "jellysquid3", 2), pack}, nil)
	if len(results) != 2 {
		t.Errorf("a mod and a modpack merged: %+v", results)
	}
}
