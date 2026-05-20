package webs

import _ "embed"

//go:embed freeai-web.zip
var staticFile []byte

func Static() []byte {
	return staticFile
}
