package encoder

import (
	"encoding/json"
)

type  JSONEncoder struct {

}

func (j * JSONEncoder) Encode(msg map[string]interface{}) (string, error) {
	b, err := json.Marshal(msg)
	outmsg := string(b)
	return outmsg,err
}

func NewJSONEncoder() JSONEncoder {
	temp := JSONEncoder{}
	return temp
}

func (e * JSONEncoder) String() string {
	return "json"
}