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

package mocks

import "github.com/stretchr/testify/mock"

type FakeStruct struct {
	mock.Mock
}

func (f *FakeStruct) Write(p []byte) (n int, err error) {
	args := f.Called(p)
	return args.Int(0), args.Error(1)
}

func (f *FakeStruct) Close() error {
	args := f.Called()
	return args.Error(0)
}

func (f *FakeStruct) Read(p []byte) (n int, err error) {
	args := f.Called(p)
	return args.Int(0), args.Error(1)
}

func (f *FakeStruct) ReadByte() (byte, error) {
	args := f.Called()
	return args.Get(0).(byte), args.Error(1)
}
