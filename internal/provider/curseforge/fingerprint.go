package curseforge

import (
	"context"
	"encoding/binary"
)

const (
	murmurM    = 0x5bd1e995
	murmurR    = 24
	murmurSeed = 1
)

// Fingerprint is CurseForge's file fingerprint: MurmurHash2 with seed 1 over the file with tabs,
// line feeds, carriage returns and spaces removed, the length being what is left.
func Fingerprint(data []byte) uint32 {
	kept := make([]byte, 0, len(data))
	for _, c := range data {
		if c != 9 && c != 10 && c != 13 && c != 32 {
			kept = append(kept, c)
		}
	}
	h := uint32(murmurSeed) ^ uint32(len(kept))
	for len(kept) >= 4 {
		k := binary.LittleEndian.Uint32(kept)
		k *= murmurM
		k ^= k >> murmurR
		k *= murmurM
		h *= murmurM
		h ^= k
		kept = kept[4:]
	}
	switch len(kept) {
	case 3:
		h ^= uint32(kept[2]) << 16
		fallthrough
	case 2:
		h ^= uint32(kept[1]) << 8
		fallthrough
	case 1:
		h ^= uint32(kept[0])
		h *= murmurM
	}
	h ^= h >> 13
	h *= murmurM
	h ^= h >> 15
	return h
}

type Match struct {
	ModID    int
	FileID   int
	FileName string
}

// MatchFingerprints finds the CurseForge files with exactly these fingerprints.
func (c *CurseForge) MatchFingerprints(ctx context.Context, fingerprints []uint32) (map[uint32]Match, error) {
	var res struct {
		Data struct {
			ExactMatches []struct {
				File struct {
					ID              int    `json:"id"`
					ModID           int    `json:"modId"`
					FileName        string `json:"fileName"`
					FileFingerprint uint32 `json:"fileFingerprint"`
				} `json:"file"`
			} `json:"exactMatches"`
		} `json:"data"`
	}
	if err := c.call(ctx, "fingerprints", func() error {
		return c.Client.PostJSON(ctx, c.BaseURL+"/fingerprints", map[string]any{"fingerprints": fingerprints}, &res)
	}); err != nil {
		return nil, err
	}
	matches := map[uint32]Match{}
	for _, m := range res.Data.ExactMatches {
		matches[m.File.FileFingerprint] = Match{ModID: m.File.ModID, FileID: m.File.ID, FileName: m.File.FileName}
	}
	return matches, nil
}
