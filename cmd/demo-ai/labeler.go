package main

import (
	"ax-distiller/internal/structure"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	_ "embed"
)

//go:embed label_prompt.txt
var label_prompt string

type Labeler struct {
	ctx         context.Context
	logger      *slog.Logger
	driver      *sql.DB
	persistent  *structure.Persistent
	persistLock *sync.Mutex
}

func newLabeler(
	ctx context.Context,
	logger *slog.Logger,
	driver *sql.DB,
	persistent *structure.Persistent,
	persistLock *sync.Mutex,
) Labeler {
	return Labeler{
		ctx,
		logger,
		driver,
		persistent,
		persistLock,
	}
}

func (l Labeler) lookupStructures(hash uint64) (structures []*structure.Structure, ok bool) {
	defer l.persistLock.Unlock()
	l.persistLock.Lock()
	structures, ok = l.persistent.Index[hash]
	if !ok {
		return
	}
	return
}

func (l Labeler) anyNonCyclicStructure(structures []*structure.Structure) (sct *structure.Structure) {
	if len(structures) == 0 {
		panic("structures should have len > 0")
	}
	sct = structures[0]
	for _, s := range structures {
		if s.FirstChild == nil {
			continue
		}
		// we check that the first child is a different hash to ensure we do
		// not get any synthetic lists whose hash matches its children
		if s.FirstChild.Hash == s.Hash {
			continue
		}
		sct = s
		break
	}
	return
}

func (l Labeler) childLabels(sct *structure.Structure) (labels []string, err error) {
	type entryResult struct {
		idx  int
		text string
		err  error
	}

	results := make(chan entryResult)
	defer close(results)

	idx := 0
	for child := sct.FirstChild; child != nil; child = child.NextSibling {
		go func(idx int, hash uint64) {
			childLabel, err := l.Label(hash)
			results <- entryResult{
				idx: idx,
				text: fmt.Sprintf("[%v] %v",
					child.Underlying.Underlying.Role.Value,
					childLabel),
				err: err,
			}
		}(idx, child.Hash)
		idx++
	}

	labels = make([]string, idx)
	for range idx {
		res := <-results
		if res.err != nil {
			err = res.err
			return
		}
		labels[res.idx] = res.text
	}
	return
}

func (l Labeler) labelWithLLM(
	sct *structure.Structure,
	childLabels []string,
) (label string, err error) {
	var body strings.Builder

	fmt.Fprintf(
		&body,
		"[%v] %v\n\n",
		sct.Underlying.Underlying.Role.Value,
		sct.Underlying.Underlying.Name.Value,
	)
	body.WriteString("children:\n")
	body.WriteString(strings.Join(childLabels, "\n"))

	fmt.Printf("%v\n\n", body.String())

	label, err = ask(l.ctx, 16, label_prompt, body.String())
	return
}

// aim for a certain min-max range of context for labelling?

func (l Labeler) Label(hash uint64) (label string, err error) {
	structures, ok := l.lookupStructures(hash)
	if !ok {
		return
	}
	exists, ok, err := LookupLabel(l.ctx, l.driver, hash)
	if err != nil {
		return
	}
	if ok {
		label = exists
		return
	}
	sct := l.anyNonCyclicStructure(structures)
	childLabels, err := l.childLabels(sct)
	if err != nil {
		return
	}
	label, err = l.labelWithLLM(sct, childLabels)
	if err != nil {
		return
	}
	err = RecordLabel(l.ctx, hash, label)
	if err != nil {
		return
	}
	l.logger.Info("label created", "hash", hash, "label", label)
	return
}
