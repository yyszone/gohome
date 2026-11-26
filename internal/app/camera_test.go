package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateCameraURL(t *testing.T) {
	valid := []string{"http://192.168.31.20:8080/video.mjpg", "https://camera.example/live/stream.m3u8?token=abc"}
	for _, input := range valid {
		if got, err := validateCameraURL(input); err != nil || got != input {
			t.Errorf("validateCameraURL(%q)=%q,%v", input, got, err)
		}
	}
	for _, input := range []string{"rtsp://192.168.31.20/live", "javascript:alert(1)", "https:///missing-host", "http://user:pass@camera.local/video"} {
		if _, err := validateCameraURL(input); err == nil {
			t.Errorf("expected invalid URL %q", input)
		}
	}
	if got, err := validateCameraURL(" "); err != nil || got != "" {
		t.Fatalf("empty URL should clear: %q %v", got, err)
	}
}

func TestUpdateDeviceCameraURL(t *testing.T) {
	a := testApp(t)
	d := Device{ID: "camera-1", Name: "门口摄像头", Category: "摄像头", Brand: "小米", Protocol: "xiaomi-cloud"}
	if err := a.store.putDevice(d); err != nil {
		t.Fatal(err)
	}
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/devices/camera-1", strings.NewReader(body))
		resp := httptest.NewRecorder()
		a.ServeHTTP(resp, req)
		return resp
	}
	if got := request(`{"cameraUrl":"http://192.168.31.20:8080/video.mjpg"}`); got.Code != http.StatusOK {
		t.Fatalf("valid camera URL status=%d %s", got.Code, got.Body.String())
	}
	if saved := a.store.snapshot().Devices[0].CameraURL; saved == "" {
		t.Fatal("camera URL was not persisted")
	}
	if got := request(`{"cameraUrl":"rtsp://192.168.31.20/live"}`); got.Code != http.StatusBadRequest {
		t.Fatalf("RTSP URL status=%d want=400", got.Code)
	}
	if got := request(`{"cameraUrl":""}`); got.Code != http.StatusOK {
		t.Fatalf("clear camera URL status=%d %s", got.Code, got.Body.String())
	}
	if saved := a.store.snapshot().Devices[0].CameraURL; saved != "" {
		t.Fatalf("camera URL not cleared: %q", saved)
	}
}
