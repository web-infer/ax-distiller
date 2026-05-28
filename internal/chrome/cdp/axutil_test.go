package cdp

import (
	"slices"
	"testing"
)

func TestFilterShallowDescendentsIter(t *testing.T) {
	// root
	// ├── ignoredA
	// │   ├── matchA1
	// │   └── ignoredA2
	// │       └── matchA2a
	// ├── matchB
	// │   └── matchB1 // must not yield; ancestor matched
	// └── ignoredC
	//     └── matchC1

	root := &AXNodeWithRelatives{}
	ignoredA := &AXNodeWithRelatives{}
	matchA1 := &AXNodeWithRelatives{}
	ignoredA2 := &AXNodeWithRelatives{}
	matchA2a := &AXNodeWithRelatives{}
	matchB := &AXNodeWithRelatives{}
	matchB1 := &AXNodeWithRelatives{}
	ignoredC := &AXNodeWithRelatives{}
	matchC1 := &AXNodeWithRelatives{}

	root.FirstChild = ignoredA
	ignoredA.NextSibling = matchB
	matchB.NextSibling = ignoredC

	ignoredA.FirstChild = matchA1
	matchA1.NextSibling = ignoredA2
	ignoredA2.FirstChild = matchA2a

	matchB.FirstChild = matchB1

	ignoredC.FirstChild = matchC1

	matching := map[*AXNodeWithRelatives]bool{
		matchA1:  true,
		matchA2a: true,
		matchB:   true,
		matchB1:  true,
		matchC1:  true,
	}

	got := slices.Collect(FilterDescendentsShallow(
		func(n *AXNodeWithRelatives) bool {
			return matching[n]
		},
		root,
	))

	want := []*AXNodeWithRelatives{
		matchA1,
		matchA2a,
		matchB,
		matchC1,
	}

	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
