package types

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type TypesTestSuite struct {
	suite.Suite
}

func (s *TypesTestSuite) SetupTest() {
}

func TestTypesSuite(t *testing.T) {
	suite.Run(t, new(TypesTestSuite))
}
