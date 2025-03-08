package fixture

import (
	"math/rand"
	"strconv"
)

var (
	RouterIP             = "127.0.0." + strconv.Itoa(rand.Intn(256))
	RouterIntegrationURL = "http://127.0.0.1:31021"
)
