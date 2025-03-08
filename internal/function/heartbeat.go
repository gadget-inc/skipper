package function

import "time"

type Heartbeat struct {
	Function  Function  `json:"function"`
	Timestamp time.Time `json:"timestamp"`
	Requests  int       `json:"requests"`
}
