package discovery

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/logger"
)

const (
	defaultPortResolverHost = "127.0.0.1"
	wildcardPortHost        = "0.0.0.0"
)

// PortResolver 通用端口解析器。
// 根据 PortConfig 配置，自动探测可用端口，支持端口冲突回退。
type PortResolver struct {
	serviceName string
}

// NewPortResolver 创建端口解析器。
func NewPortResolver(serviceName string) *PortResolver {
	return &PortResolver{serviceName: strings.TrimSpace(serviceName)}
}

// Resolve 根据 PortConfig 解析可用端口。
func (r *PortResolver) Resolve(portCfg config.PortConfig) (int, error) {
	preferred := portCfg.GetPort()
	if preferred > 0 && IsPortAvailable("", preferred) {
		return preferred, nil
	}

	serviceLabel := r.serviceLabel()
	if preferred > 0 {
		logger.Warn("%s默认端口 %d 已占用", serviceLabel, preferred)
		if !portCfg.IsAutoFallbackEnabled() {
			return 0, fmt.Errorf("%s端口 %d 已占用且未启用自动回退", serviceLabel, preferred)
		}
	}

	rangeMin, rangeMax, ok := portCfg.RangeBounds()
	if !ok {
		return 0, fmt.Errorf("%s端口范围配置无效: %v", serviceLabel, portCfg.Range)
	}

	retryCount := portCfg.BindRetryCount
	if retryCount <= 0 {
		retryCount = 3
	}
	retryDelay := time.Duration(portCfg.BindRetryDelay) * time.Millisecond
	if retryDelay <= 0 {
		retryDelay = time.Second
	}

	for attempt := 0; attempt < retryCount; attempt++ {
		for candidate := rangeMin; candidate <= rangeMax; candidate++ {
			if candidate == preferred {
				continue
			}
			if !IsPortAvailable("", candidate) {
				continue
			}
			if preferred > 0 {
				logger.Warn("%s自动回退到可用端口 %d", serviceLabel, candidate)
			} else {
				logger.Info("%s选择范围内可用端口 %d", serviceLabel, candidate)
			}
			return candidate, nil
		}
		if attempt < retryCount-1 {
			logger.Info("%s第 %d 次扫描未找到可用端口，等待 %v 后重试...", serviceLabel, attempt+1, retryDelay)
			time.Sleep(retryDelay)
		}
	}

	return 0, fmt.Errorf("%s在范围 [%d, %d] 内未找到可用端口（已重试 %d 次）", serviceLabel, rangeMin, rangeMax, retryCount)
}

// IsPortAvailable 检查端口是否可用。
func IsPortAvailable(host string, port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return canListenOnPort(defaultPortResolverHost, port) && canListenOnPort(wildcardPortHost, port)
	}
	return canListenOnPort(host, port)
}

func canListenOnPort(host string, port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

func (r *PortResolver) serviceLabel() string {
	if r == nil || strings.TrimSpace(r.serviceName) == "" {
		return "服务"
	}
	return strings.TrimSpace(r.serviceName) + " 服务"
}
