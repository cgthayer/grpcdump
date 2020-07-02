package framereader

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/rmedvedev/grpcdump/internal/app/grpc"
	"github.com/rmedvedev/grpcdump/internal/app/models"
	"golang.org/x/net/http2"
)

//FrameReader ...
type FrameReader struct {
	framer  *http2.Framer
	Streams *Streams
	paths   *sync.Map
}

//New creates FrameReader
func New(framer *http2.Framer, paths *sync.Map) *FrameReader {
	return &FrameReader{framer, NewStreams(), paths}
}

//Read ...
func (frameReader *FrameReader) Read(packet *models.Packet) (models.RenderModel, error) {
	//trying to read http2 frame
	frame, err := frameReader.framer.ReadFrame()
	if err == io.EOF {
		return nil, nil
	}

	if err != nil {
		return nil, errors.New(fmt.Sprint("Error reading frame: ", err))
	}

	connKey := packet.GetConnectionKey()
	streamID := frame.Header().StreamID
	var stream *models.Stream

	switch frame := frame.(type) {
	case *http2.MetaHeadersFrame:
		metaHeaders := make(map[string]string)
		for _, hf := range frame.Fields {
			metaHeaders[hf.Name] = hf.Value
			if hf.Name == ":path" {
				stream = &models.Stream{
					ID:   streamID,
					Path: hf.Value,
					Type: models.RequestType,
				}

				frameReader.Streams.Add(
					connKey,
					stream,
				)

				frameReader.paths.Store(packet.GetConnectionKey(), hf.Value)
			} else if hf.Name == ":status" {
				stream = &models.Stream{
					ID:   streamID,
					Type: models.ResponseType,
				}

				if path, ok := frameReader.paths.Load(packet.GetRevConnectionKey()); ok {
					stream.Path = path.(string)
				}

				frameReader.Streams.Add(
					connKey,
					stream,
				)
			}
		}

		if stream != nil {
			stream.MetaHeaders = metaHeaders
		}
	case *http2.DataFrame:

		stream, _ := frameReader.Streams.Get(connKey, streamID)

		grpcMessage, err := grpc.Decode(stream.Path, frame, stream.Type, &stream.GrpcState)
		if err != nil {
			return nil, err
		}

		var http2Model models.RenderModel
		switch stream.Type {
		case models.RequestType:
			http2Model = models.NewHttp2Request(packet, stream, grpcMessage)
		case models.ResponseType:
			http2Model = models.NewHttp2Response(packet, stream, grpcMessage)
		}

		return http2Model, nil
	}

	return nil, nil
}
