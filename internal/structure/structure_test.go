package structure

import (
	"ax-distiller/internal/chrome/cdp"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/zeebo/xxh3"
)

func parseLayer(str string) (ret *cdp.AXNodeWithRelatives) {
	var prev *cdp.AXNodeWithRelatives
	for _, c := range str {
		if prev == nil {
			prev = &cdp.AXNodeWithRelatives{
				AXNode: &cdp.AXNode{
					Role: cdp.Value[string]{Value: string(c)},
				},
			}
			ret = prev
			continue
		}
		node := &cdp.AXNodeWithRelatives{
			AXNode: &cdp.AXNode{
				Role: cdp.Value[string]{Value: string(c)},
			},
		}
		prev.NextSibling = node
		prev = node
	}

	return
}

func h(hashes ...uint64) (combinationHash uint64) {
	var buff []byte
	for _, h := range hashes {
		buff = binary.LittleEndian.AppendUint64(buff, h)
	}
	combinationHash = xxh3.Hash(buff)
	return
}

func l(str string) (combinationHash uint64) {
	return xxh3.Hash([]byte(str))
}

func printDebug(expected []uint64, output *Structure, out io.Writer) {
	fmt.Fprintln(out, "Expected array:")
	for i := range expected {
		fmt.Fprint(out, expected[i], ", ")
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Got array:")
	fmt.Fprintln(out, output.String())
	fmt.Fprintln(out)
}

func TestConstructFlat(t *testing.T) {
	type testCase struct {
		input    string
		expected []uint64
	}

	table := []testCase{
		{
			input:    "ABAB",
			expected: []uint64{h(l("A"), l("B"))},
		},
		{
			input:    "ABABC",
			expected: []uint64{h(l("A"), l("B")), l("C")},
		},
		{
			input:    "FABBBABBABG",
			expected: []uint64{l("F"), h(l("A"), l("B")), l("G")},
		},
		{
			input:    "ABCABC",
			expected: []uint64{h(l("A"), l("B"), l("C"))},
		},
		{
			// 1 - banner
			// 2 - heading (text)
			// 3 - link (image + text)
			// 4 - link (text)
			// 5 - iframe
			// 6 - button
			// 7 - listitem (link)
			// 8 - separator
			input: "1233334233334233334233334245627827",
			expected: []uint64{
				l("1"),
				h(
					l("2"),
					l("3"),
					l("4"),
				),
				l("2"),
				l("4"),
				l("5"),
				l("6"),
				h(
					l("2"),
					l("7"),
				),
				l("8"),
				h(
					l("2"),
					l("7"),
				),
			},
		},
	}

	var head *Structure
	for _, tc := range table {
		output := Construct(parseLayer(tc.input))
		head = output
		itr := 0
		for output != nil && itr < len(tc.expected) {
			if tc.expected[itr] != output.Hash {
				var out strings.Builder
				printDebug(tc.expected, head, &out)
				t.Fatal("Hash is incorrect test failed.", out.String())
			}
			output = output.NextSibling
			itr++
		}

		if output != nil || itr < len(tc.expected) {
			var out strings.Builder
			printDebug(tc.expected, head, &out)
			t.Fatal("Output and expected are not the same length.", out.String())
		}
	}
}
