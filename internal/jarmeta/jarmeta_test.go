package jarmeta

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestReadFabricSkipsByteOrderMark(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("fabric.mod.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("\xef\xbb\xbf" + `{"schemaVersion":1,"id":"bommed","version":"1.0","environment":"client"}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	info, err := readZip(zr)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "bommed" || info.Side != "client" {
		t.Fatalf("parsed %+v", info)
	}
}
