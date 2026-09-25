package mojang

import (
	"context"
	"fmt"
	"strings"

	"shulker.sh/shulker/internal/fetch"
)

const (
	ProfileAPIURL     = "https://api.mojang.com"
	ProfileSessionURL = "https://sessionserver.mojang.com"
	profileBulkLimit  = 10
)

// Profiles reads player profiles from Mojang's profile API and session server.
type Profiles struct {
	Client     *fetch.Client
	APIURL     string
	SessionURL string
}

func NewProfiles(c *fetch.Client) *Profiles {
	return &Profiles{Client: c, APIURL: ProfileAPIURL, SessionURL: ProfileSessionURL}
}

type Profile struct {
	Name string
	UUID string
}

type apiProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ByNames is the profile of each name Mojang knows, keyed by the name in lowercase.
func (p *Profiles) ByNames(ctx context.Context, names []string) (map[string]Profile, error) {
	found := map[string]Profile{}
	for start := 0; start < len(names); start += profileBulkLimit {
		end := min(start+profileBulkLimit, len(names))
		var profiles []apiProfile
		if err := p.Client.PostJSON(ctx, p.APIURL+"/profiles/minecraft", names[start:end], &profiles); err != nil {
			return nil, fmt.Errorf("mojang name lookup: %w", err)
		}
		for _, ap := range profiles {
			found[strings.ToLower(ap.Name)] = Profile{Name: ap.Name, UUID: Dashed(ap.ID)}
		}
	}
	return found, nil
}

// ByUUID is the profile behind a uuid, false when Mojang has none.
func (p *Profiles) ByUUID(ctx context.Context, uuid string) (Profile, bool, error) {
	var ap apiProfile
	url := p.SessionURL + "/session/minecraft/profile/" + strings.ReplaceAll(uuid, "-", "")
	ok, err := p.Client.GetJSONIfFound(ctx, url, &ap)
	if err != nil {
		return Profile{}, false, fmt.Errorf("mojang profile lookup: %w", err)
	}
	if !ok {
		return Profile{}, false, nil
	}
	return Profile{Name: ap.Name, UUID: Dashed(ap.ID)}, true, nil
}

// Dashed is a uuid in lowercase with its four dashes. An id that isn't 32 characters once
// undashed comes back lowercased with no dashes at all.
func Dashed(id string) string {
	id = strings.ToLower(strings.ReplaceAll(id, "-", ""))
	if len(id) != 32 {
		return id
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}
