package main

import (
	"errors"
	"slices"
	"strings"
)

const (
	sourceClaude = "claude"
	sourceCodex  = "codex"
	sourceAgy    = "agy"
)

type Catalog struct {
	store     *Store
	artifacts *artifactStore
	indexes   []*TranscriptIndex
}

func NewCatalog(store *Store, artifacts *artifactStore, indexes ...*TranscriptIndex) *Catalog {
	c := &Catalog{store: store, artifacts: artifacts}
	for _, idx := range indexes {
		if idx != nil {
			c.indexes = append(c.indexes, idx)
		}
	}
	return c
}

func (c *Catalog) HasSessions() bool {
	return len(c.indexes) > 0
}

func (c *Catalog) Sources() []string {
	var out []string
	if c.store != nil {
		out = append(out, sourceClaude)
	}
	for _, idx := range c.indexes {
		if s := idx.format.source(); !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func (c *Catalog) Plans() ([]Plan, error) {
	var plans []Plan
	var storeErr, artifactErr error
	if c.store != nil {
		plans, storeErr = c.store.List()
	}
	if c.artifacts != nil {
		var artifacts []Plan
		artifacts, artifactErr = c.artifacts.List()
		plans = append(plans, artifacts...)
	}
	for _, idx := range c.indexes {
		idx.Refresh()
		plans = append(plans, idx.Plans()...)
	}
	slices.SortFunc(plans, func(a, b Plan) int { return b.Updated.Compare(a.Updated) })
	return plans, errors.Join(storeErr, artifactErr)
}

func (c *Catalog) Plan(name string) (Plan, error) {
	if validName(name) {
		if c.store == nil {
			return Plan{}, errNotFound
		}
		return c.store.Get(name)
	}
	if strings.Contains(name, "/") {
		if c.artifacts == nil {
			return Plan{}, errNotFound
		}
		return c.artifacts.Get(name)
	}
	for _, idx := range c.indexes {
		idx.Refresh()
		if p, ok := idx.Plan(name); ok {
			return p, nil
		}
	}
	return Plan{}, errNotFound
}

func (c *Catalog) Sessions() []Session {
	var out []Session
	for _, idx := range c.indexes {
		idx.Refresh()
		out = append(out, idx.Sessions()...)
	}
	slices.SortFunc(out, func(a, b Session) int { return b.Updated.Compare(a.Updated) })
	return out
}

func (c *Catalog) Session(id string) (Session, bool) {
	for _, idx := range c.indexes {
		idx.Refresh()
		if s, ok := idx.Session(id); ok {
			return s, true
		}
	}
	return Session{}, false
}
