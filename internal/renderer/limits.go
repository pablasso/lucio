package renderer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
)

const (
	MaxSceneBytes  = 1 << 20
	MaxAssetBytes  = 64 << 20
	MaxAssetPixels = 32_000_000
	MaxAssetSide   = 16_384
	MaxFontSize    = 512
	maxJSONDepth   = 32
	maxJSONTokens  = 16_384
	maxStringBytes = 4096
	maxNumber      = 4096
)

// Bound recursion, work, and arithmetic BEFORE allocating the scene tree.
// Duplicate fields are rejected instead of silently normalized.
func checkSceneLimits(data []byte) *Issue {
	if len(data) > MaxSceneBytes {
		i := errorIssue("", "", "resource_limit", "scene exceeds 1 MiB")
		return &i
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	tokens := 0
	var walk func(int, string) error
	walk = func(depth int, path string) error {
		tokens++
		if depth > maxJSONDepth || tokens > maxJSONTokens {
			return fmt.Errorf("%s: JSON nesting/token limit exceeded", path)
		}
		v, err := d.Token()
		if err != nil {
			return err
		}
		switch v := v.(type) {
		case string:
			if len(v) > maxStringBytes {
				return fmt.Errorf("%s: string exceeds %d bytes", path, maxStringBytes)
			}
		case json.Number:
			n, err := v.Float64()
			if err != nil || math.Abs(n) > maxNumber {
				return fmt.Errorf("%s: numbers must be within ±%d", path, maxNumber)
			}
		case json.Delim:
			switch v {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, err := d.Token()
					if err != nil {
						return err
					}
					key, ok := k.(string)
					if !ok {
						return fmt.Errorf("invalid object key")
					}
					if seen[key] {
						return fmt.Errorf("%s: duplicate field %q", path, key)
					}
					seen[key] = true
					if len(key) > maxStringBytes {
						return fmt.Errorf("object key too long")
					}
					if err := walk(depth+1, joinPath(path, key)); err != nil {
						return err
					}
				}
			case '[':
				for i := 0; d.More(); i++ {
					if err := walk(depth+1, path+"/"+strconv.Itoa(i)); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("unexpected delimiter")
			}
			_, err := d.Token()
			return err
		}
		return nil
	}
	if err := walk(0, ""); err != nil {
		i := errorIssue("", "", "invalid_json", err.Error())
		return &i
	}
	if _, err := d.Token(); err != io.EOF {
		i := errorIssue("", "", "invalid_json", "scene must contain one JSON document")
		return &i
	}
	return nil
}

func validImageSize(w, h int) bool {
	return w > 0 && h > 0 && w <= MaxAssetSide && h <= MaxAssetSide && w <= MaxAssetPixels/h
}
