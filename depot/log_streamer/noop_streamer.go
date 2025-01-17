package log_streamer

import (
	"io"
	"io/ioutil"
)

type noopStreamer struct{}

func NewNoopStreamer() LogStreamer {
	return noopStreamer{}
}

func (noopStreamer) Stdout() io.Writer                 { return ioutil.Discard }
func (noopStreamer) Stderr() io.Writer                 { return ioutil.Discard }
func (noopStreamer) Flush()                            {}
func (noopStreamer) UpdateTags(tags map[string]string) {}

func (noopStreamer) WithSource(sourceName string) LogStreamer {
	return noopStreamer{}
}
func (noopStreamer) SourceName() string { return DefaultLogSource }
func (noopStreamer) Stop()              {}
