package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

var levelNames = map[LogLevel]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

// Logger 日志记录器
type Logger struct {
	level        LogLevel
	logger       *log.Logger
	file         *os.File
	mu           sync.Mutex
	component    string
	maxSize      int64 // 最大文件大小（字节）
	maxBackups   int   // 最大备份文件数
	maxAge       int   // 最大保留天数（0表示不限制）
	maxTotalSize int64 // 所有日志文件的最大总大小（字节，0表示不限制）
	compress     bool  // 是否压缩旧日志
	logDir       string // 日志目录
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// Init 初始化默认日志记录器
func Init(logDir string, level LogLevel, component string) error {
	var err error
	once.Do(func() {
		defaultLogger, err = NewLogger(logDir, level, component)
	})
	return err
}

// LoggerConfig 日志配置
type LoggerConfig struct {
	MaxSize      int64 // 单个日志文件最大大小（字节），默认 10MB
	MaxBackups   int   // 最大备份文件数，默认 5
	MaxAge       int   // 最大保留天数，0 表示不限制
	MaxTotalSize int64 // 所有日志文件最大总大小（字节），0 表示不限制
	Compress     bool  // 是否压缩旧日志，默认 false
}

// DefaultLoggerConfig 返回默认日志配置
func DefaultLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		MaxSize:      10 * 1024 * 1024, // 10MB
		MaxBackups:   5,
		MaxAge:       30,                // 30天
		MaxTotalSize: 100 * 1024 * 1024, // 100MB
		Compress:     false,
	}
}

// NewLogger 创建新的日志记录器
func NewLogger(logDir string, level LogLevel, component string) (*Logger, error) {
	return NewLoggerWithConfig(logDir, level, component, DefaultLoggerConfig())
}

// NewLoggerWithConfig 使用自定义配置创建日志记录器
func NewLoggerWithConfig(logDir string, level LogLevel, component string, config *LoggerConfig) (*Logger, error) {
	// 创建日志目录
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// 创建日志文件
	logFile := filepath.Join(logDir, fmt.Sprintf("%s.log", component))
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// 只输出到文件，避免与 subprocess 的 stdout/stderr 重定向冲突
	l := &Logger{
		level:        level,
		logger:       log.New(file, "", 0),
		file:         file,
		component:    component,
		logDir:       logDir,
		maxSize:      config.MaxSize,
		maxBackups:   config.MaxBackups,
		maxAge:       config.MaxAge,
		maxTotalSize: config.MaxTotalSize,
		compress:     config.Compress,
	}

	// 启动时清理旧日志
	l.cleanOldLogs()

	return l, nil
}

// Close 关闭日志记录器
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// SetLevel 设置日志级别
func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// log 内部日志方法
func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	// 检查文件大小并轮转
	l.rotateIfNeeded()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	levelName := levelNames[level]
	message := fmt.Sprintf(format, args...)

	logLine := fmt.Sprintf("[%s] [%s] [%s] %s\n", timestamp, levelName, l.component, message)
	l.logger.Print(logLine)

	// FATAL 级别日志后退出程序
	if level == FATAL {
		// 确保日志写入磁盘
		if l.file != nil {
			l.file.Sync()
		}
		os.Exit(1)
	}
}

// rotateIfNeeded 检查并轮转日志文件
func (l *Logger) rotateIfNeeded() {
	if l.file == nil {
		return
	}

	info, err := l.file.Stat()
	if err != nil {
		return
	}

	if info.Size() < l.maxSize {
		return
	}

	// 关闭当前文件
	l.file.Close()

	// 轮转备份文件
	logFile := filepath.Join(l.logDir, fmt.Sprintf("%s.log", l.component))

	// 删除最旧的备份（如果超过最大备份数）
	if l.maxBackups > 0 {
		oldestBackup := fmt.Sprintf("%s.%d", logFile, l.maxBackups)
		if l.compress {
			oldestBackup += ".gz"
		}
		os.Remove(oldestBackup)
	}

	// 轮转现有备份文件
	for i := l.maxBackups - 1; i > 0; i-- {
		oldName := fmt.Sprintf("%s.%d", logFile, i)
		newName := fmt.Sprintf("%s.%d", logFile, i+1)

		if l.compress {
			oldName += ".gz"
			newName += ".gz"
		}

		if _, err := os.Stat(oldName); err == nil {
			os.Rename(oldName, newName)
		}
	}

	// 重命名当前日志文件
	backupName := fmt.Sprintf("%s.1", logFile)
	os.Rename(logFile, backupName)

	// 如果启用压缩，压缩备份文件
	if l.compress {
		go l.compressFile(backupName)
	}

	// 创建新的日志文件
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}

	l.file = file
	l.logger.SetOutput(file)

	// 清理旧日志
	l.cleanOldLogs()
}

// Debug 调试日志
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

// Info 信息日志
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

// Warn 警告日志
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(WARN, format, args...)
}

// Error 错误日志
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

// Fatal 致命错误日志
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(FATAL, format, args...)
}

// 默认日志记录器的便捷方法
func Debug(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Debug(format, args...)
	}
}

func Info(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Info(format, args...)
	}
}

func Warn(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Warn(format, args...)
	}
}

func Error(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Error(format, args...)
	}
}

func Fatal(format string, args ...interface{}) {
	if defaultLogger != nil {
		defaultLogger.Fatal(format, args...)
	}
}

// ParseLevel 解析日志级别字符串
func ParseLevel(levelStr string) LogLevel {
	switch levelStr {
	case "DEBUG":
		return DEBUG
	case "INFO":
		return INFO
	case "WARN":
		return WARN
	case "ERROR":
		return ERROR
	case "FATAL":
		return FATAL
	default:
		return INFO
	}
}

// cleanOldLogs 清理旧日志文件
func (l *Logger) cleanOldLogs() {
	if l.logDir == "" {
		return
	}

	// 获取所有日志文件
	logFiles := l.getLogFiles()
	if len(logFiles) == 0 {
		return
	}

	// 按时间清理
	if l.maxAge > 0 {
		l.cleanByAge(logFiles)
	}

	// 按总大小清理
	if l.maxTotalSize > 0 {
		l.cleanByTotalSize(logFiles)
	}
}

// getLogFiles 获取所有日志文件（包括备份和压缩文件）
func (l *Logger) getLogFiles() []logFileInfo {
	pattern := filepath.Join(l.logDir, fmt.Sprintf("%s.log*", l.component))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}

	var files []logFileInfo
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		files = append(files, logFileInfo{
			path:    path,
			size:    info.Size(),
			modTime: info.ModTime(),
		})
	}

	return files
}

// logFileInfo 日志文件信息
type logFileInfo struct {
	path    string
	size    int64
	modTime time.Time
}

// cleanByAge 按时间清理日志文件
func (l *Logger) cleanByAge(files []logFileInfo) {
	cutoff := time.Now().AddDate(0, 0, -l.maxAge)
	for _, file := range files {
		if file.modTime.Before(cutoff) {
			os.Remove(file.path)
		}
	}
}

// cleanByTotalSize 按总大小清理日志文件
func (l *Logger) cleanByTotalSize(files []logFileInfo) {
	// 按修改时间排序（最新的在前）
	sortFilesByTime(files)

	var totalSize int64
	for _, file := range files {
		totalSize += file.size
		if totalSize > l.maxTotalSize {
			os.Remove(file.path)
		}
	}
}

// sortFilesByTime 按修改时间排序（最新的在前）
func sortFilesByTime(files []logFileInfo) {
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[i].modTime.Before(files[j].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
}

// compressFile 压缩日志文件
func (l *Logger) compressFile(filename string) error {
	// 检查文件是否存在
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil
	}

	// 打开源文件
	src, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer src.Close()

	// 创建压缩文件
	dst, err := os.Create(filename + ".gz")
	if err != nil {
		return err
	}
	defer dst.Close()

	// 创建 gzip writer
	gzWriter := gzip.NewWriter(dst)
	defer gzWriter.Close()

	// 复制数据
	if _, err := io.Copy(gzWriter, src); err != nil {
		return err
	}

	// 删除原文件
	return os.Remove(filename)
}

// GetLogStats 获取日志统计信息
func (l *Logger) GetLogStats() map[string]interface{} {
	files := l.getLogFiles()

	var totalSize int64
	var compressedCount int
	var uncompressedCount int

	for _, file := range files {
		totalSize += file.size
		if strings.HasSuffix(file.path, ".gz") {
			compressedCount++
		} else {
			uncompressedCount++
		}
	}

	return map[string]interface{}{
		"total_files":       len(files),
		"total_size":        totalSize,
		"compressed_files":  compressedCount,
		"uncompressed_files": uncompressedCount,
		"max_size":          l.maxSize,
		"max_backups":       l.maxBackups,
		"max_age":           l.maxAge,
		"max_total_size":    l.maxTotalSize,
	}
}
