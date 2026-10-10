package main

import "testing"

func TestParseNAV(t *testing.T) {
	// header and date are Shift_JIS in the real file
	csv := "\x94N\x8c\x8e\x93\xfa,\x8a\xee\x8f\x80\x89\xbf\x8az,x,y,z\r\n" +
		"2026\x94N10\x8c\x8e08\x93\xfa,38277,14168523,,\r\n" +
		"2026\x94N10\x8c\x8e09\x93\xfa,38109,14138653,,\r\n"
	v, err := parseNAV([]byte(csv))
	if err != nil || v != 38109 {
		t.Fatalf("got %v, %v", v, err)
	}
	if _, err := parseNAV([]byte("a,b,c,d,e\r\n")); err == nil {
		t.Fatal("expected error for header-only CSV")
	}
}
