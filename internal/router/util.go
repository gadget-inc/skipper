package router

func panicIf(err error) {
	if err != nil {
		panic(err)
	}
}

func unwrap[V any](v V, err error) V {
	panicIf(err)
	return v
}
