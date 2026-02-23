package executor

import "encoding/json"

func parseConfig(raw json.RawMessage, dst any) error {
if len(raw) == 0 {
return nil
}
return json.Unmarshal(raw, dst)
}
