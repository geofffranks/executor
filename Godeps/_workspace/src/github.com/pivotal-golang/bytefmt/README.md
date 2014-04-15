# BYTEFMT
Human readable byte formatter for Go.

Example:

```go
  bytefmt.ByteSize(100.5*bytefmt.MEGABYE) // returns "100.5M"
  bytefmt.ByteSize(uint64(1024)) // returns "1K"
```

For documentation, please see http://godoc.org/github.com/pivotal-golang/bytefmt