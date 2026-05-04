package doclint

import (
	"sort"
	"testing"

	"gotest.tools/v3/assert"
)

func TestFlagReferenceSync_FlagInStructMissingFromDocsTable(t *testing.T) {
	t.Parallel()

	configGo := []byte(`package controller

import "time"

type Config struct {
	Host  string        ` + "`flag:\"host\" description:\"the host\" default:\"0.0.0.0\"`" + `
	Port  int           ` + "`flag:\"port\" description:\"the port\" default:\"50051\"`" + `
	Quiet time.Duration ` + "`flag:\"quiet\" description:\"silence\" default:\"5s\"`" + `
}
`)

	docMd := []byte(`---
title: Configuration Reference
---

## Controller configuration

| Flag      | Type     | Default   | Description |
| --------- | -------- | --------- | ----------- |
| ` + "`--host`" + ` | string   | ` + "`0.0.0.0`" + ` | host        |
| ` + "`--port`" + ` | int      | ` + "`50051`" + `   | port        |

## Router configuration

| Flag | Type | Default | Description |
| ---- | ---- | ------- | ----------- |
`)

	got, err := scanFlagReferenceSync(
		[]flagSyncPair{{
			Section:  "Controller configuration",
			ConfigGo: File{Path: "internal/controller/config.go", Content: configGo},
		}},
		File{Path: "docs/content/reference/configuration.md", Content: docMd},
		nil,
	)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 1, "expected one missing-from-docs finding for `quiet`")
	assert.Equal(t, got[0].Token, "--quiet")
	assert.Equal(t, got[0].Rule, "flag-reference-sync")
	assert.Equal(t, got[0].File, "docs/content/reference/configuration.md")
	// Section header is on line 5 -- the finding points at the header so
	// the operator knows where to add the missing row.
	assert.Equal(t, got[0].Line, 5)
}

func TestFlagReferenceSync_FlagInDocsTableMissingFromStruct(t *testing.T) {
	t.Parallel()

	configGo := []byte(`package controller

type Config struct {
	Host string ` + "`flag:\"host\"`" + `
}
`)

	docMd := []byte(`## Controller configuration

| Flag       | Type   | Default | Description |
| ---------- | ------ | ------- | ----------- |
| ` + "`--host`" + `   | string |         | host        |
| ` + "`--ghost`" + `  | string |         | not real    |
`)

	got, err := scanFlagReferenceSync(
		[]flagSyncPair{{
			Section:  "Controller configuration",
			ConfigGo: File{Path: "internal/controller/config.go", Content: configGo},
		}},
		File{Path: "docs/content/reference/configuration.md", Content: docMd},
		nil,
	)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 1)
	assert.Equal(t, got[0].Token, "--ghost")
	// The extra-in-docs finding points at the row line (line 6 here).
	assert.Equal(t, got[0].Line, 6)
}

func TestFlagReferenceSync_AllowlistSuppressesMissingFlag(t *testing.T) {
	t.Parallel()

	configGo := []byte(`package controller

type Config struct {
	Host    string ` + "`flag:\"host\"`" + `
	DevOnly string ` + "`flag:\"dev-only\"`" + `
}
`)

	docMd := []byte(`## Controller configuration

| Flag      | Type   | Default | Description |
| --------- | ------ | ------- | ----------- |
| ` + "`--host`" + ` | string |         | host        |
`)

	got, err := scanFlagReferenceSync(
		[]flagSyncPair{{
			Section:  "Controller configuration",
			ConfigGo: File{Path: "internal/controller/config.go", Content: configGo},
		}},
		File{Path: "docs/content/reference/configuration.md", Content: docMd},
		[]flagSyncAllowEntry{{Section: "Controller configuration", Flag: "dev-only"}},
	)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0, "allowlist must suppress dev-only flags")
}

func TestFlagReferenceSync_TwoSectionsScannedIndependently(t *testing.T) {
	t.Parallel()

	controllerGo := []byte(`package controller

type Config struct {
	Host string ` + "`flag:\"host\"`" + `
}
`)
	routerGo := []byte(`package router

type Config struct {
	Port int ` + "`flag:\"port\"`" + `
}
`)

	docMd := []byte(`## Controller configuration

| Flag      | Type | Default | Description |
| --------- | ---- | ------- | ----------- |

## Router configuration

| Flag      | Type | Default | Description |
| --------- | ---- | ------- | ----------- |
`)

	got, err := scanFlagReferenceSync(
		[]flagSyncPair{
			{Section: "Controller configuration", ConfigGo: File{Path: "internal/controller/config.go", Content: controllerGo}},
			{Section: "Router configuration", ConfigGo: File{Path: "internal/router/config.go", Content: routerGo}},
		},
		File{Path: "docs/content/reference/configuration.md", Content: docMd},
		nil,
	)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 2)

	tokens := []string{got[0].Token, got[1].Token}
	sort.Strings(tokens)
	assert.DeepEqual(t, tokens, []string{"--host", "--port"})
}

func TestFlagReferenceSync_NoFindingsWhenInSync(t *testing.T) {
	t.Parallel()

	configGo := []byte(`package controller

type Config struct {
	Host string ` + "`flag:\"host\"`" + `
	Port int    ` + "`flag:\"port\"`" + `
}
`)

	docMd := []byte(`## Controller configuration

| Flag      | Type   | Default | Description |
| --------- | ------ | ------- | ----------- |
| ` + "`--host`" + ` | string |         | host        |
| ` + "`--port`" + ` | int    |         | port        |
`)

	got, err := scanFlagReferenceSync(
		[]flagSyncPair{{
			Section:  "Controller configuration",
			ConfigGo: File{Path: "internal/controller/config.go", Content: configGo},
		}},
		File{Path: "docs/content/reference/configuration.md", Content: docMd},
		nil,
	)
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)
}
