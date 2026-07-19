package logging

import "github.com/sirupsen/logrus"

var log = logrus.WithFields(logrus.Fields{
	"service": "gms",
})

func Logger() *logrus.Entry {
	return log
}
