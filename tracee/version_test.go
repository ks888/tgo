package tracee

import "testing"

func TestParseGoVersion(t *testing.T) {
	for i, testdata := range []struct {
		input  string
		expect GoVersion
	}{
		{input: "go1.11.1", expect: GoVersion{Raw: "go1.11.1", MajorVersion: 1, MinorVersion: 11, PatchVersion: 1}},
		{input: "go1.11", expect: GoVersion{Raw: "go1.11", MajorVersion: 1, MinorVersion: 11}},
		{input: "devel", expect: GoVersion{Raw: "devel", Devel: true}},
	} {
		actual := ParseGoVersion(testdata.input)
		if actual != testdata.expect {
			t.Errorf("[%d] wrong result: %v", i, actual)
		}
	}
}

func TestGoVersion_LaterThan(t *testing.T) {
	for i, testdata := range []struct {
		a, b   GoVersion
		expect bool
	}{
		{
			a:      GoVersion{Devel: true},
			b:      GoVersion{MajorVersion: 1},
			expect: true,
		},
		{
			a:      GoVersion{MajorVersion: 2},
			b:      GoVersion{MajorVersion: 1},
			expect: true,
		},
		{
			a:      GoVersion{MajorVersion: 0},
			b:      GoVersion{MajorVersion: 1},
			expect: false,
		},
		{
			a:      GoVersion{MajorVersion: 1, MinorVersion: 12},
			b:      GoVersion{MajorVersion: 1, MinorVersion: 11},
			expect: true,
		},
		{
			a:      GoVersion{MajorVersion: 1, MinorVersion: 10},
			b:      GoVersion{MajorVersion: 1, MinorVersion: 11},
			expect: false,
		},
		{
			a:      GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 1},
			b:      GoVersion{MajorVersion: 1, MinorVersion: 11},
			expect: true,
		},
		{
			a:      GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 1},
			b:      GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 1},
			expect: true,
		},
		{
			a:      GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 1},
			b:      GoVersion{MajorVersion: 1, MinorVersion: 11, PatchVersion: 2},
			expect: false,
		},
	} {
		actual := testdata.a.LaterThan(testdata.b)
		if actual != testdata.expect {
			t.Errorf("[%d] wrong result: %v", i, actual)
		}
	}
}
