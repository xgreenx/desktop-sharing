package sharingnode

import (
	"encoding/binary"
	"io"
	"sync"
)

type DataWriter struct {
	sync.Mutex
	sizes  []int
	data   []byte
	writer io.Writer
	signal chan struct{}
	Error  chan error
}

func NewDataWriter(writer io.Writer) *DataWriter {
	data := &DataWriter{
		sizes:  make([]int, 0),
		data:   make([]byte, 0),
		writer: writer,
		signal: make(chan struct{}, 1),
		Error:  make(chan error, 1),
	}

	go data.write()

	return data
}

func (q *DataWriter) AddData(data []byte) {
	select {
	case q.signal <- struct{}{}:
	default:
	}

	if len(data) == 0 {
		return
	}

	q.Lock()
	q.sizes = append(q.sizes, len(data))
	q.data = append(q.data, data...)
	q.Unlock()
}

func (q *DataWriter) write() {
	for {
		<-q.signal

	Repeat:
		q.Lock()
		if len(q.sizes) == 0 {
			q.Unlock()
			continue
		}

		tmp := make([]byte, 4+len(q.sizes)*4+len(q.data))
		binary.LittleEndian.PutUint32(tmp[:4], uint32(len(q.sizes)))

		for i, _ := range q.sizes {
			binary.LittleEndian.PutUint32(tmp[4*(i+1):4*(i+2)], uint32(q.sizes[i]))
		}
		copy(tmp[4*(len(q.sizes)+1):], q.data)
		q.sizes = []int{}
		q.data = []byte{}
		q.Unlock()

		_, err := q.writer.Write(tmp)
		if err != nil {
			q.Error <- err
			return
		} else {
			goto Repeat
		}
	}
}

type DataReader struct {
	reader io.Reader
	dataCh chan []byte
	error  error
}

func NewDataReader(reader io.Reader) *DataReader {
	data := &DataReader{
		reader: reader,
		dataCh: make(chan []byte, 1024),
	}

	go data.read()

	return data
}

func (q *DataReader) GetData() ([]byte, error) {
	data, ok := <-q.dataCh
	if !ok {
		return nil, q.error
	}

	return data, nil
}

func (q *DataReader) read() {
	readData := func(data []byte) error {
		done := 0
		for done < len(data) {
			n, err := q.reader.Read(data[done:])
			if err != nil {
				return err
			}
			done += n
		}
		return nil
	}
	readInt := func() (int, error) {
		v := make([]byte, 4)
		err := readData(v)
		if err != nil {
			return 0, err
		}

		return int(binary.LittleEndian.Uint32(v)), nil
	}

	for {
		var sizes []int
		frames, err := readInt()
		if err != nil {
			goto Error
		}

		sizes = make([]int, frames)
		for i := 0; i < frames; i++ {
			dataLen, err := readInt()
			if err != nil {
				goto Error
			}
			sizes[i] = dataLen
		}

		for i := 0; i < len(sizes); i++ {
			data := make([]byte, sizes[i])
			err = readData(data)
			if err != nil {
				goto Error
			}

			q.dataCh <- data
		}

		continue
	Error:

		q.error = err
		close(q.dataCh)
		break
	}
}
