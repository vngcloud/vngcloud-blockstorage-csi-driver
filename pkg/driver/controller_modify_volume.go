package driver

import (
	"context"
	"errors"
	"fmt"
	"github.com/vngcloud/vngcloud-blockstorage-csi-driver/pkg/cloud"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"sync"
	"time"
)

type modifyVolumeManager struct {
	requestHandlerMap sync.Map
}

func newModifyVolumeManager() *modifyVolumeManager {
	return &modifyVolumeManager{
		requestHandlerMap: sync.Map{},
	}
}

type modifyVolumeResponse struct {
	volumeSize int64
	err        error
}

type modifyVolumeRequest struct {
	newSize           int64
	modifyDiskOptions cloud.ModifyDiskOptions
	// Channel for sending the response to the request caller
	responseChan chan modifyVolumeResponse
}

func (s *controllerService) modifyVolumeWithCoalescing(ctx context.Context, volume string, options *cloud.ModifyDiskOptions) error {
	responseChan := make(chan modifyVolumeResponse)
	request := modifyVolumeRequest{
		modifyDiskOptions: *options,
		responseChan:      responseChan,
	}

	// Intentionally not pass in context as we deal with context locally in this method
	s.addModifyVolumeRequest(volume, &request) //nolint:contextcheck

	select {
	case response := <-responseChan:
		if response.err != nil {
			if errors.Is(response.err, cloud.ErrInvalidArgument) {
				return status.Errorf(codes.InvalidArgument, "Could not modify volume %q: %v", volume, response.err)
			}
			return status.Errorf(codes.Internal, "Could not modify volume %q: %v", volume, response.err)
		}
	case <-ctx.Done():
		return status.Errorf(codes.Internal, "Could not modify volume %q: context cancelled", volume)
	}

	return nil
}

// When a new request comes in, we look up requestHandlerMap using the volume ID of the request.
// If there's no ModifyVolumeRequestHandler for the volume, meaning that there’s no inflight requests for the volume, we will start a goroutine
// for the volume calling processModifyVolumeRequests method, and ModifyVolumeRequestHandler for the volume will be added to requestHandlerMap.
// If there’s ModifyVolumeRequestHandler for the volume, meaning that there is inflight request(s) for the volume, we will send the new request
// to the goroutine for the volume via the receiving channel.
// Note that each volume with inflight requests has their own goroutine which follows timeout schedule of their own.
func (s *controllerService) addModifyVolumeRequest(volumeID string, r *modifyVolumeRequest) {
	requestHandler := newModifyVolumeRequestHandler(volumeID, r)
	handler, loaded := s.modifyVolumeManager.requestHandlerMap.LoadOrStore(volumeID, requestHandler)
	if loaded {
		h := handler.(modifyVolumeRequestHandler)
		h.requestChan <- r
	} else {
		responseChans := []chan modifyVolumeResponse{r.responseChan}
		go s.processModifyVolumeRequests(&requestHandler, responseChans)
	}
}

func newModifyVolumeRequestHandler(volumeID string, request *modifyVolumeRequest) modifyVolumeRequestHandler {
	requestChan := make(chan *modifyVolumeRequest)
	return modifyVolumeRequestHandler{
		requestChan:   requestChan,
		mergedRequest: request,
		volumeID:      volumeID,
	}
}

type modifyVolumeRequestHandler struct {
	volumeID string
	// Merged request from the requests that have been accepted for the volume
	mergedRequest *modifyVolumeRequest
	// Channel for sending requests to the goroutine for the volume
	requestChan chan *modifyVolumeRequest
}

func (s *controllerService) processModifyVolumeRequests(h *modifyVolumeRequestHandler, responseChans []chan modifyVolumeResponse) {
	klog.V(4).InfoS("Start processing ModifyVolumeRequest for ", "volume ID", h.volumeID)
	process := func(req *modifyVolumeRequest) {
		if err := h.validateModifyVolumeRequest(req); err != nil {
			req.responseChan <- modifyVolumeResponse{err: err}
		} else {
			h.mergeModifyVolumeRequest(req)
			responseChans = append(responseChans, req.responseChan)
		}
	}

	for {
		select {
		case req := <-h.requestChan:
			process(req)
		case <-time.After(s.driverOptions.modifyVolumeRequestHandlerTimeout):
			s.modifyVolumeManager.requestHandlerMap.Delete(h.volumeID)
			// At this point, no new requests can come in on the request channel because it has been removed from the map
			// However, the request channel may still have requests waiting on it
			// Thus, process any requests still waiting in the channel
			for loop := true; loop; {
				select {
				case req := <-h.requestChan:
					process(req)
				default:
					loop = false
				}
			}
			actualSizeGiB, err := s.executeModifyVolumeRequest(h.volumeID, h.mergedRequest)
			for _, c := range responseChans {
				select {
				case c <- modifyVolumeResponse{volumeSize: actualSizeGiB, err: err}:
				default:
					klog.V(6).InfoS("Ignoring response channel because it has no receiver", "volumeID", h.volumeID)
				}
			}
			return
		}
	}
}

// This function validates the new request against the merged request for the volume.
// If the new request has a volume property that's already included in the merged request and its value is different from that in the merged request,
// this function will return an error and the new request will be rejected.
func (h *modifyVolumeRequestHandler) validateModifyVolumeRequest(r *modifyVolumeRequest) error {
	if r.newSize != 0 && h.mergedRequest.newSize != 0 && r.newSize != h.mergedRequest.newSize {
		return fmt.Errorf("Different size was requested by a previous request. Current: %d, Requested: %d", h.mergedRequest.newSize, r.newSize)
	}
	if r.modifyDiskOptions.VolumeType != "" && h.mergedRequest.modifyDiskOptions.VolumeType != "" && r.modifyDiskOptions.VolumeType != h.mergedRequest.modifyDiskOptions.VolumeType {
		return fmt.Errorf("Different volume type was requested by a previous request. Current: %s, Requested: %s", h.mergedRequest.modifyDiskOptions.VolumeType, r.modifyDiskOptions.VolumeType)
	}
	return nil
}

func (h *modifyVolumeRequestHandler) mergeModifyVolumeRequest(r *modifyVolumeRequest) {
	if r.newSize != 0 {
		h.mergedRequest.newSize = r.newSize
	}
	if r.modifyDiskOptions.VolumeType != "" {
		h.mergedRequest.modifyDiskOptions.VolumeType = r.modifyDiskOptions.VolumeType
	}
}

func (s *controllerService) executeModifyVolumeRequest(volumeID string, req *modifyVolumeRequest) (int64, error) {
	_, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	actualSizeGiB, err := s.cloud.ResizeOrModifyDisk(volumeID, req.newSize, &req.modifyDiskOptions)
	if err != nil {
		return 0, err
	} else {
		return actualSizeGiB, nil
	}
}
