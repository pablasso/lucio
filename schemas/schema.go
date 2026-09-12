package schemas

import _ "embed"

//go:embed lucio.scene.v1.json
var sceneV1 []byte

func SceneV1JSON() []byte {
	return append([]byte(nil), sceneV1...)
}
