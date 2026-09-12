package renderer

import (
	"fmt"

	"github.com/pablasso/lucio/schemas"
)

func SchemaJSON(version string) ([]byte, error) {
	if version != SceneVersionV1 {
		return nil, fmt.Errorf("unsupported scene version %q", version)
	}
	return schemas.SceneV1JSON(), nil
}
