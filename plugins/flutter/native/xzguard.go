package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// DictCap in ulikunitz/xz is a minimum, not a ceiling. Walk the actual LZMA2
// chunk framing before decoding so every real block header is checked, including
// blocks hidden by a forged index. The library still validates all checksums,
// index records and footer during extraction. No compression is implemented here.
func preflightXZ(ctx context.Context, f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 24 || info.Size() > limits.DownloadBytes {
		return fmt.Errorf("invalid XZ size")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	read := func(n int) ([]byte, error) {
		buf := make([]byte, n)
		_, e := io.ReadFull(contextReader{ctx, f}, buf)
		return buf, e
	}
	skip := func(n int64) error {
		pos, e := f.Seek(0, io.SeekCurrent)
		if e != nil {
			return e
		}
		if n < 0 || n > info.Size()-pos {
			return io.ErrUnexpectedEOF
		}
		_, e = f.Seek(n, io.SeekCurrent)
		return e
	}
	h, err := read(12)
	if err != nil {
		return err
	}
	if !bytes.Equal(h[:6], []byte{0xfd, '7', 'z', 'X', 'Z', 0}) || h[6] != 0 || crc32.ChecksumIEEE(h[6:8]) != binary.LittleEndian.Uint32(h[8:]) {
		return fmt.Errorf("invalid XZ header")
	}
	check := int64(0)
	switch h[7] {
	case 1:
		check = 4
	case 4:
		check = 8
	case 10:
		check = 32
	default:
		return fmt.Errorf("XZ integrity check required")
	}
	var expanded int64
	for blocks := 0; blocks <= 4096; blocks++ {
		first, err := read(1)
		if err != nil {
			return err
		}
		if first[0] == 0 {
			if blocks == 0 {
				return fmt.Errorf("empty XZ stream")
			}
			pos, err := f.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}
			if info.Size()-pos > 128<<10 {
				return fmt.Errorf("XZ index/trailing data exceeds limit")
			}
			return nil
		}
		if blocks == 4096 {
			return fmt.Errorf("too many XZ blocks")
		}
		size := (int(first[0]) + 1) * 4
		tail, err := read(size - 1)
		if err != nil {
			return err
		}
		header := append(first, tail...)
		if crc32.ChecksumIEEE(header[:size-4]) != binary.LittleEndian.Uint32(header[size-4:]) {
			return fmt.Errorf("invalid XZ block header checksum")
		}
		flags := header[1]
		if flags&0x3f != 0 {
			return fmt.Errorf("only a single LZMA2 filter is supported")
		}
		fields := bytes.NewReader(header[2 : size-4])
		number := func() (uint64, error) {
			v, e := binary.ReadUvarint(fields)
			if e != nil || v > 1<<63-1 {
				return 0, fmt.Errorf("invalid XZ integer")
			}
			return v, nil
		}
		var declaredC, declaredU uint64
		if flags&0x40 != 0 {
			declaredC, err = number()
			if err != nil {
				return err
			}
		}
		if flags&0x80 != 0 {
			declaredU, err = number()
			if err != nil {
				return err
			}
		}
		id, err := number()
		if err != nil {
			return err
		}
		propSize, err := number()
		if err != nil {
			return err
		}
		if id != 0x21 || propSize != 1 {
			return fmt.Errorf("unsupported XZ filter")
		}
		prop, err := fields.ReadByte()
		if err != nil {
			return err
		}
		// LZMA2 property 28 is 64 MiB; 40 encodes almost 4 GiB.
		if prop > 28 {
			return fmt.Errorf("XZ dictionary exceeds 64 MiB")
		}
		padding, _ := io.ReadAll(fields)
		for _, v := range padding {
			if v != 0 {
				return fmt.Errorf("nonzero XZ header padding")
			}
		}
		compressed, uncompressed := int64(0), int64(0)
		for {
			ctl, err := read(1)
			if err != nil {
				return err
			}
			compressed++
			if ctl[0] == 0 {
				break
			}
			var c, u int64
			switch {
			case ctl[0] == 1 || ctl[0] == 2:
				v, err := read(2)
				if err != nil {
					return err
				}
				compressed += 2
				c = int64(binary.BigEndian.Uint16(v)) + 1
				u = c
			case ctl[0] >= 0x80:
				v, err := read(4)
				if err != nil {
					return err
				}
				compressed += 4
				u = int64(ctl[0]&31)<<16 | int64(binary.BigEndian.Uint16(v[:2]))
				u++
				c = int64(binary.BigEndian.Uint16(v[2:])) + 1
				if ctl[0] >= 0xc0 {
					if _, err = read(1); err != nil {
						return err
					}
					compressed++
				}
			default:
				return fmt.Errorf("invalid LZMA2 control byte")
			}
			compressed += c
			uncompressed += u
			expanded += u
			if expanded > limits.ExpandedBytes+int64(limits.Entries)*4096+1024 {
				return fmt.Errorf("XZ expanded stream exceeds limit")
			}
			if err = skip(c); err != nil {
				return err
			}
		}
		if flags&0x40 != 0 && declaredC != uint64(compressed) || flags&0x80 != 0 && declaredU != uint64(uncompressed) {
			return fmt.Errorf("XZ block size mismatch")
		}
		if err = skip((4-compressed%4)%4 + check); err != nil {
			return err
		}
	}
	return fmt.Errorf("invalid XZ stream")
}
