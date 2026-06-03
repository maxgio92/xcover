package pkg

//go:noinline
func Helper() int { return internal() + 7 }

//go:noinline
func internal() int { return 3 }
