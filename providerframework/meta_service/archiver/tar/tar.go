package tar

import (
	"archive/tar"
	log "github.com/sirupsen/logrus"
	"time"

	"github.com/soda-cdm/kahu/providerframework/meta_service/archiver"
)

type tarArchiver struct {
	writer archiver.Writer
	tar    *tar.Writer
}

func NewArchiver(writer archiver.Writer) archiver.Archiver {
	return &tarArchiver{
		writer: writer,
		tar:    tar.NewWriter(writer),
	}
}

func (archiver *tarArchiver) WriteFile(file string, data []byte) error {
	hdr := &tar.Header{
		Name:     file,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
		Mode:     0755,
		ModTime:  time.Now(),
	}

	log.Infof("writing header %+v", hdr)
	if err := archiver.tar.WriteHeader(hdr); err != nil {
		return err
	}

	log.Infof("writing data %s", data)
	if _, err := archiver.tar.Write(data); err != nil {
		return err
	}
	return nil
}

func (archiver *tarArchiver) Close() error {
	err := archiver.tar.Close()
	if err != nil {
		return err
	}

	err = archiver.writer.Close()
	if err != nil {
		return err
	}

	return nil
}
