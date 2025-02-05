// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package services

import (
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/googleapis/gapic-showcase/server"
	pb "github.com/googleapis/gapic-showcase/server/genproto"
	lropb "google.golang.org/genproto/googleapis/longrunning"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// NewEchoServer returns a new EchoServer for the Showcase API.
func NewEchoServer() pb.EchoServer {
	return &echoServerImpl{waiter: server.GetWaiterInstance()}
}

type echoServerImpl struct {
	waiter server.Waiter
}

func (s *echoServerImpl) Echo(ctx context.Context, in *pb.EchoRequest) (*pb.EchoResponse, error) {
	err := status.ErrorProto(in.GetError())
	if err != nil {
		return nil, err
	}
	echoTrailers(ctx)
	return &pb.EchoResponse{Content: in.GetContent(), Severity: in.GetSeverity()}, nil
}

func (s *echoServerImpl) Expand(in *pb.ExpandRequest, stream pb.Echo_ExpandServer) error {
	for _, word := range strings.Fields(in.GetContent()) {
		err := stream.Send(&pb.EchoResponse{Content: word})
		if err != nil {
			return err
		}
	}
	if in.GetError() != nil {
		return status.ErrorProto(in.GetError())
	}
	echoStreamingTrailers(stream)
	return nil
}

func (s *echoServerImpl) Collect(stream pb.Echo_CollectServer) error {
	var resp []string

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			echoStreamingTrailers(stream)
			return stream.SendAndClose(&pb.EchoResponse{Content: strings.Join(resp, " ")})
		}
		if err != nil {
			return err
		}
		s := status.ErrorProto(req.GetError())
		if s != nil {
			return s
		}
		if req.GetContent() != "" {
			resp = append(resp, req.GetContent())
		}
	}
}

func (s *echoServerImpl) Chat(stream pb.Echo_ChatServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			echoStreamingTrailers(stream)
			return nil
		}
		if err != nil {
			return err
		}

		s := status.ErrorProto(req.GetError())
		if s != nil {
			return s
		}
		stream.Send(&pb.EchoResponse{Content: req.GetContent()})
	}
}

func (s *echoServerImpl) PagedExpandLegacy(ctx context.Context, in *pb.PagedExpandLegacyRequest) (*pb.PagedExpandResponse, error) {
	req := &pb.PagedExpandRequest{
		Content:   in.Content,
		PageSize:  in.MaxResults,
		PageToken: in.PageToken,
	}
	return s.PagedExpand(ctx, req)
}

func (s *echoServerImpl) PagedExpand(ctx context.Context, in *pb.PagedExpandRequest) (*pb.PagedExpandResponse, error) {
	if in.GetPageSize() < 0 {
		return nil, status.Error(codes.InvalidArgument, "The page size provided must not be negative.")
	}
	words := strings.Fields(in.GetContent())

	start := int32(0)
	if in.GetPageToken() != "" {
		token, err := strconv.Atoi(in.GetPageToken())
		token32 := int32(token)
		if err != nil || token32 < 0 || token32 >= int32(len(words)) {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"Invalid page token: %s. Token must be within the range [0, %d)",
				in.GetPageToken(),
				len(words))
		}
		start = token32
	}

	pageSize := in.GetPageSize()
	if pageSize == 0 {
		pageSize = int32(len(words))
	}
	end := min(start+pageSize, int32(len(words)))

	responses := []*pb.EchoResponse{}
	for _, word := range words[start:end] {
		responses = append(responses, &pb.EchoResponse{Content: word})
	}

	nextToken := ""
	if end < int32(len(words)) {
		nextToken = strconv.Itoa(int(end))
	}

	echoTrailers(ctx)
	return &pb.PagedExpandResponse{
		Responses:     responses,
		NextPageToken: nextToken,
	}, nil
}

func min(x int32, y int32) int32 {
	if x < y {
		return x
	}
	return y
}

func (s *echoServerImpl) Wait(ctx context.Context, in *pb.WaitRequest) (*lropb.Operation, error) {
	echoTrailers(ctx)
	return s.waiter.Wait(in), nil
}

func (s *echoServerImpl) Block(ctx context.Context, in *pb.BlockRequest) (*pb.BlockResponse, error) {
	d, _ := ptypes.Duration(in.GetResponseDelay())
	time.Sleep(d)
	if in.GetError() != nil {
		return nil, status.ErrorProto(in.GetError())
	}
	echoTrailers(ctx)
	return in.GetSuccess(), nil
}

// echo any provided trailing metadata
func echoTrailers(ctx context.Context) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return
	}

	values := md.Get("showcase-trailer")
	for _, value := range values {
		trailer := metadata.Pairs("showcase-trailer", value)
		grpc.SetTrailer(ctx, trailer)
	}
}

func echoStreamingTrailers(stream grpc.ServerStream) {
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return
	}

	values := md.Get("showcase-trailer")
	for _, value := range values {
		trailer := metadata.Pairs("showcase-trailer", value)
		stream.SetTrailer(trailer)
	}
}
