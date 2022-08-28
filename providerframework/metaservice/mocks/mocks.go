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

type FakeStruct struct{}

func (f *FakeStruct) Write(p []byte) (n int, err error) {
	return 1, nil
}

func (f *FakeStruct) Close() error {
	return nil
}

func (f *FakeStruct) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (f *FakeStruct) ReadByte() (byte, error) {
	return 1, nil
}
