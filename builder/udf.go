package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
)

const (
	udfTagAVDP           = 2
	udfTagPartition      = 5
	udfTagLogicalVolume  = 6
	udfTagTerminating    = 8
	udfTagFileSet        = 256
	udfTagFileIdentifier = 257
	udfTagFileEntry      = 261
	udfTagExtFileEntry   = 266
	udfAllocShort        = 0
	udfAllocLong         = 1
	udfAllocEmbedded     = 3
)

type udfVol struct {
	r              io.ReaderAt
	partitionStart uint32
	rootICB        uint32
}

type udfDirent struct {
	name  string
	isDir bool
	icb   uint32
}

type udfFileEntry struct {
	size      uint32
	relLBA    uint32
	allocType int
	embedded  []byte
}

// listUDFPath walks UDF 1.02 (Windows eval ISOs: NSR02, no Joliet, ISO9660 stub)
// for efi/microsoft/boot using case-insensitive names.
func listUDFPath(r io.ReaderAt, parts []string) ([]isoExtent, error) {
	vol, err := openUDF(r)
	if err != nil {
		return nil, err
	}
	cur := vol.rootICB
	for _, p := range parts {
		ents, err := vol.readDir(cur)
		if err != nil {
			return nil, err
		}
		var next *udfDirent
		for i := range ents {
			if !ents[i].isDir {
				continue
			}
			if isoNamesEqual(ents[i].name, p) {
				next = &ents[i]
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("missing directory %q", p)
		}
		cur = next.icb
	}
	ents, err := vol.readDir(cur)
	if err != nil {
		return nil, err
	}
	var files []isoExtent
	for _, e := range ents {
		if e.isDir || e.name == "" {
			continue
		}
		ext, err := vol.fileExtent(e.icb)
		if err != nil {
			return nil, err
		}
		ext.name = e.name
		files = append(files, ext)
	}
	return files, nil
}

func openUDF(r io.ReaderAt) (*udfVol, error) {
	var avdp []byte
	for _, lba := range []uint32{256, 512} {
		sec, err := readSectorAt(r, lba)
		if err != nil {
			continue
		}
		if binary.LittleEndian.Uint16(sec[0:2]) == udfTagAVDP {
			avdp = sec
			break
		}
	}
	if avdp == nil {
		return nil, fmt.Errorf("no UDF AVDP at sector 256 or 512")
	}
	vds := binary.LittleEndian.Uint32(avdp[20:24])
	if vds == 0 {
		return nil, fmt.Errorf("UDF AVDP has empty volume descriptor sequence")
	}

	var partStart, fsdLBN uint32
	sawPart, sawLVD := false, false
	for i := uint32(0); i < 32; i++ {
		sec, err := readSectorAt(r, vds+i)
		if err != nil {
			return nil, fmt.Errorf("read UDF VDS: %w", err)
		}
		tag := binary.LittleEndian.Uint16(sec[0:2])
		switch tag {
		case udfTagPartition:
			partStart = binary.LittleEndian.Uint32(sec[188:192])
			sawPart = true
		case udfTagLogicalVolume:
			fsdLBN = binary.LittleEndian.Uint32(sec[252:256])
			sawLVD = true
		case udfTagTerminating, 0:
			i = 32
		}
	}
	if !sawPart || !sawLVD {
		return nil, fmt.Errorf("UDF volume descriptors missing partition or logical volume")
	}

	fsd, err := readSectorAt(r, partStart+fsdLBN)
	if err != nil {
		return nil, fmt.Errorf("read UDF file set: %w", err)
	}
	if binary.LittleEndian.Uint16(fsd[0:2]) != udfTagFileSet {
		return nil, fmt.Errorf("UDF file set tag %d", binary.LittleEndian.Uint16(fsd[0:2]))
	}
	rootICB := binary.LittleEndian.Uint32(fsd[404:408])
	return &udfVol{r: r, partitionStart: partStart, rootICB: rootICB}, nil
}

func (v *udfVol) readDir(icb uint32) ([]udfDirent, error) {
	data, _, err := v.icbPayload(icb)
	if err != nil {
		return nil, err
	}
	var out []udfDirent
	for off := 0; off+38 <= len(data); {
		if data[off] == 0 && data[off+1] == 0 {
			break
		}
		tag := binary.LittleEndian.Uint16(data[off : off+2])
		if tag != 0 && tag != udfTagFileIdentifier {
			break
		}
		lFI := int(data[off+19])
		lIU := int(binary.LittleEndian.Uint16(data[off+36 : off+38]))
		recLen := 38 + lIU + lFI
		recLen = (recLen + 3) &^ 3
		if recLen == 0 || off+recLen > len(data) {
			break
		}
		chars := data[off+18]
		identOff := off + 38 + lIU
		name := decodeOSTA(data[identOff : identOff+lFI])
		icbLoc := binary.LittleEndian.Uint32(data[off+24 : off+28])
		if name != "" && chars&0x08 == 0 && chars&0x04 == 0 {
			out = append(out, udfDirent{
				name:  name,
				isDir: chars&0x02 != 0,
				icb:   icbLoc,
			})
		}
		off += recLen
	}
	return out, nil
}

func (v *udfVol) fileExtent(icb uint32) (isoExtent, error) {
	_, fe, err := v.icbPayload(icb)
	if err != nil {
		return isoExtent{}, err
	}
	if fe.allocType == udfAllocEmbedded || fe.relLBA == 0 {
		return isoExtent{}, fmt.Errorf("UDF file has no allocated extent")
	}
	return isoExtent{lba: v.partitionStart + fe.relLBA, size: fe.size}, nil
}

func (v *udfVol) icbPayload(icb uint32) ([]byte, udfFileEntry, error) {
	sec, err := readSectorAt(v.r, v.partitionStart+icb)
	if err != nil {
		return nil, udfFileEntry{}, err
	}
	fe, err := parseUDFFileEntry(sec)
	if err != nil {
		return nil, fe, err
	}
	if fe.allocType == udfAllocEmbedded {
		return fe.embedded, fe, nil
	}
	if fe.relLBA == 0 {
		return nil, fe, fmt.Errorf("UDF ICB %d has no allocation", icb)
	}
	abs := v.partitionStart + fe.relLBA
	nsec := (fe.size + isoSectorSize - 1) / isoSectorSize
	if nsec == 0 {
		nsec = 1
	}
	buf := make([]byte, nsec*isoSectorSize)
	if _, err := v.r.ReadAt(buf, int64(abs)*isoSectorSize); err != nil && err != io.EOF {
		return nil, fe, err
	}
	if int(fe.size) < len(buf) {
		buf = buf[:fe.size]
	}
	return buf, fe, nil
}

func parseUDFFileEntry(sec []byte) (udfFileEntry, error) {
	var fe udfFileEntry
	if len(sec) < 176 {
		return fe, fmt.Errorf("short UDF file entry")
	}
	tag := binary.LittleEndian.Uint16(sec[0:2])
	var infoLen uint64
	var lEA, lAD uint32
	var allocOff int
	var flags uint16
	switch tag {
	case udfTagFileEntry:
		flags = binary.LittleEndian.Uint16(sec[34:36])
		infoLen = binary.LittleEndian.Uint64(sec[56:64])
		lEA = binary.LittleEndian.Uint32(sec[168:172])
		lAD = binary.LittleEndian.Uint32(sec[172:176])
		allocOff = 176 + int(lEA)
	case udfTagExtFileEntry:
		if len(sec) < 216 {
			return fe, fmt.Errorf("short UDF extended file entry")
		}
		flags = binary.LittleEndian.Uint16(sec[34:36])
		infoLen = binary.LittleEndian.Uint64(sec[56:64])
		lEA = binary.LittleEndian.Uint32(sec[208:212])
		lAD = binary.LittleEndian.Uint32(sec[212:216])
		allocOff = 216 + int(lEA)
	default:
		return fe, fmt.Errorf("UDF file entry tag %d", tag)
	}
	fe.allocType = int(flags & 7)
	fe.size = uint32(infoLen)
	if allocOff < 0 || allocOff+int(lAD) > len(sec) {
		return fe, fmt.Errorf("UDF allocation descriptors truncated")
	}
	ads := sec[allocOff : allocOff+int(lAD)]
	switch fe.allocType {
	case udfAllocEmbedded:
		fe.embedded = ads
		if fe.size == 0 {
			fe.size = uint32(len(ads))
		}
	case udfAllocLong:
		if len(ads) < 16 {
			return fe, fmt.Errorf("UDF long_ad truncated")
		}
		fe.relLBA = binary.LittleEndian.Uint32(ads[4:8])
	default:
		if len(ads) < 8 {
			return fe, fmt.Errorf("UDF short_ad truncated")
		}
		fe.relLBA = binary.LittleEndian.Uint32(ads[4:8])
	}
	return fe, nil
}

func decodeOSTA(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case 8:
		return strings.TrimRight(string(raw[1:]), "\x00")
	case 16:
		b := raw[1:]
		if len(b)%2 == 1 {
			b = b[:len(b)-1]
		}
		u16 := make([]uint16, len(b)/2)
		for i := range u16 {
			u16[i] = binary.BigEndian.Uint16(b[i*2:])
		}
		return strings.TrimRight(string(utf16.Decode(u16)), "\x00")
	default:
		return strings.TrimRight(string(raw), "\x00")
	}
}
