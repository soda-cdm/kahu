/*
Copyright 2022 The SODA Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package backup

import (
	"fmt"
	"testing"

	log "github.com/sirupsen/logrus"
	v1beta1 "github.com/soda-cdm/kahu/apis/kahu/v1beta1"
	fake "github.com/soda-cdm/kahu/client/clientset/versioned/fake"
	informers "github.com/soda-cdm/kahu/client/informers/externalversions"
	"github.com/soda-cdm/kahu/controllers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
)

type fakeBackupLister struct {
	mock.Mock
}

func (s *fakeBackupLister) List(selector labels.Selector) ([]*v1beta1.Backup, error) {
	args := s.Called(selector)
	var list []*v1beta1.Backup
	return list, args.Error(1)
}

func (s *fakeBackupLister) Get(name string) (*v1beta1.Backup, error) {
	args := s.Called(name)
	backup := &v1beta1.Backup{}
	backup.Spec.MetadataLocation = "fakeMetaDataLocation"
	return backup, args.Error(1)
}

func TestDoBackupNonProcessed(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		expectedErr error
	}{
		{
			name:        "bad key does not return error",
			key:         "bad/key/here",
			expectedErr: fmt.Errorf("unexpected key format: \"bad/key/here\""),
		},
		{
			name:        "backup not found in lister does return error",
			key:         "backup_demo",
			expectedErr: errors.NewNotFound(v1beta1.Resource("backup"), "backup_demo"),
		},
	}

	name := "backup-test"
	logger := log.WithField("BackupController", name)
	sharedInformers := informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)
	conBuild := controllers.NewControllerBuilder(name)
	ctrl, _ := conBuild.Build()

	bpctrl := &controller{
		genericController: ctrl,
		backupLister:      sharedInformers.Kahu().V1beta1().Backups().Lister(),
		logger:            logger,
	}

	for _, test := range tests {
		err := bpctrl.doBackup(test.key)
		assert.Equal(t, test.expectedErr, err)
	}
}
