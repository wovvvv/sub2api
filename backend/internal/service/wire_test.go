package service

import (
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/zeromicro/go-zero/core/collection"
)

func TestProvideTimingWheelService_ReturnsError(t *testing.T) {
	original := newTimingWheel
	t.Cleanup(func() { newTimingWheel = original })

	newTimingWheel = func(_ time.Duration, _ int, _ collection.Execute) (*collection.TimingWheel, error) {
		return nil, errors.New("boom")
	}

	svc, err := ProvideTimingWheelService()
	if err == nil {
		t.Fatalf("期望返回 error，但得到 nil")
	}
	if svc != nil {
		t.Fatalf("期望返回 nil svc，但得到非空")
	}
}

func TestProvideTimingWheelService_Success(t *testing.T) {
	svc, err := ProvideTimingWheelService()
	if err != nil {
		t.Fatalf("期望 err 为 nil，但得到: %v", err)
	}
	if svc == nil {
		t.Fatalf("期望 svc 非空，但得到 nil")
	}
	svc.Stop()
}

func TestProvideAccountTestService_SetsSettingService(t *testing.T) {
	settingService := &SettingService{}

	svc := ProvideAccountTestService(
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
		nil,
		settingService,
	)

	if svc.settingService != settingService {
		t.Fatalf("期望 AccountTestService 注入 settingService")
	}
}

func TestProvideOpenAIGatewayService_SetsSettingService(t *testing.T) {
	settingService := &SettingService{}

	svc := ProvideOpenAIGatewayService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		settingService,
	)

	if svc.settingService != settingService {
		t.Fatalf("期望 OpenAIGatewayService 注入 settingService")
	}
}

func TestProvideGeminiMessagesCompatService_SetsSettingService(t *testing.T) {
	settingService := &SettingService{}

	svc := ProvideGeminiMessagesCompatService(
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		&config.Config{},
		settingService,
	)

	if svc.settingService != settingService {
		t.Fatalf("期望 GeminiMessagesCompatService 注入 settingService")
	}
}
