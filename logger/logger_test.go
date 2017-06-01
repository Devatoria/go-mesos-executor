package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type LoggerTestSuite struct {
	suite.Suite
	logger *Logger
}

func (s *LoggerTestSuite) SetupTest() {
	s.logger = GetInstance()
}

// Ensure the returned instance is the same than the already initialized one.
// It means that the singleton is doing what it's supposed to do: do not
// initialize a new instance of logger if already initialized.
func (s *LoggerTestSuite) TestLogger() {
	assert.Equal(s.T(), s.logger, GetInstance())
}

func TestLoggerSuite(t *testing.T) {
	suite.Run(t, new(LoggerTestSuite))
}
