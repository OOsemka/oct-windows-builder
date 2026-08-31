package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchElToritoNoprompt(t *testing.T) {
	assertNopromptPatch(t, buildMinimalWindowsISOWithNames("EFI", "MICROSOFT", "BOOT"))
}

func TestPatchElToritoNopromptMixedCaseISO9660(t *testing.T) {
	assertNopromptPatch(t, buildMinimalWindowsISOWithNames("efi", "Microsoft", "Boot"))
}

func TestPatchElToritoNopromptUDFMixedCase(t *testing.T) {
	assertNopromptPatch(t, buildMinimalUDFWindowsISO())
}

func TestPatchElToritoIdempotent(t *testing.T) {
	img := buildMinimalWindowsISOWithNames("EFI", "MICROSOFT", "BOOT")
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(path, img, 0644); err != nil {
		t.Fatal(err)
	}
	if err := patchISONoprompt(dir); err != nil {
		t.Fatal(err)
	}
	if err := patchISONoprompt(dir); err != nil {
		t.Fatal(err)
	}
}

func TestISONamesEqual(t *testing.T) {
	if !isoNamesEqual("EFI", "efi") || !isoNamesEqual("efi", "EFI") {
		t.Fatal("case-insensitive directory match")
	}
	if !isoNamesEqual("MICROSOF", "microsoft") {
		t.Fatal("ISO9660 8-char truncation for microsoft")
	}
	if !isoNamesEqual("EFISYS_N.BIN;1", "efisys_noprompt.bin") {
		t.Fatal("ISO9660 8.3 name for efisys_noprompt.bin")
	}
}

func TestResolveISOPathFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(p, make([]byte, isoSectorSize+1), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveISOPath(dir)
	if err != nil || got != p {
		t.Fatalf("got %q err %v", got, err)
	}
}

func assertNopromptPatch(t *testing.T, img []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.img")
	if err := os.WriteFile(path, img, 0644); err != nil {
		t.Fatal(err)
	}
	if err := patchISONoprompt(dir); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	prompt, noprompt, catalogLBA, err := inspectWindowsISO(f)
	if err != nil {
		t.Fatal(err)
	}
	if noprompt.lba == 0 || noprompt.size == 0 {
		t.Fatalf("noprompt lba=%d size=%d", noprompt.lba, noprompt.size)
	}
	same, err := extentsEqual(f, prompt, noprompt)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Fatal("prompt boot image should match noprompt after patch")
	}
	sec, err := readSectorAt(f, catalogLBA)
	if err != nil {
		t.Fatal(err)
	}
	got := binary.LittleEndian.Uint32(sec[96+8 : 96+12])
	if got != noprompt.lba {
		t.Fatalf("catalog EFI LBA=%d want %d", got, noprompt.lba)
	}
}

func buildMinimalWindowsISO() []byte {
	return buildMinimalWindowsISOWithNames("EFI", "MICROSOFT", "BOOT")
}

func buildMinimalWindowsISOWithNames(efi, microsoft, boot string) []byte {
	const sectors = 48
	img := make([]byte, sectors*isoSectorSize)

	pvd := img[16*isoSectorSize : 17*isoSectorSize]
	pvd[0] = isoPVDType
	copy(pvd[1:], isoStdID)
	putDirRecord(pvd, 156, "\x00", 20, isoSectorSize, true)

	bootRec := img[17*isoSectorSize : 18*isoSectorSize]
	bootRec[0] = isoBootType
	copy(bootRec[1:], isoStdID)
	copy(bootRec[7:], "EL TORITO SPECIFICATION")
	binary.LittleEndian.PutUint32(bootRec[71:75], 25)

	term := img[18*isoSectorSize : 19*isoSectorSize]
	term[0] = isoTerminatorType
	copy(term[1:], isoStdID)

	root := img[20*isoSectorSize : 21*isoSectorSize]
	off := 0
	off = putDirRecord(root, off, "\x00", 20, isoSectorSize, true)
	off = putDirRecord(root, off, "\x01", 20, isoSectorSize, true)
	putDirRecord(root, off, efi, 21, isoSectorSize, true)

	efiDir := img[21*isoSectorSize : 22*isoSectorSize]
	off = 0
	off = putDirRecord(efiDir, off, "\x00", 21, isoSectorSize, true)
	off = putDirRecord(efiDir, off, "\x01", 20, isoSectorSize, true)
	putDirRecord(efiDir, off, microsoft, 22, isoSectorSize, true)

	ms := img[22*isoSectorSize : 23*isoSectorSize]
	off = 0
	off = putDirRecord(ms, off, "\x00", 22, isoSectorSize, true)
	off = putDirRecord(ms, off, "\x01", 21, isoSectorSize, true)
	putDirRecord(ms, off, boot, 23, isoSectorSize, true)

	const fileSize = 4 * isoSectorSize
	bootDir := img[23*isoSectorSize : 24*isoSectorSize]
	off = 0
	off = putDirRecord(bootDir, off, "\x00", 23, isoSectorSize, true)
	off = putDirRecord(bootDir, off, "\x01", 22, isoSectorSize, true)
	off = putDirRecord(bootDir, off, "EFISYS.BIN;1", 30, fileSize, false)
	putDirRecord(bootDir, off, "EFISYS_NOPROMPT.BIN;1", 34, fileSize, false)

	putElToritoCatalog(img[25*isoSectorSize:26*isoSectorSize], 30, fileSize)

	for i := 0; i < fileSize; i++ {
		img[30*isoSectorSize+i] = 0xAA
		img[34*isoSectorSize+i] = 0xBB
	}
	return img
}

func putElToritoCatalog(cat []byte, promptLBA, fileSize uint32) {
	cat[0] = 0x01
	cat[30] = 0x55
	cat[31] = 0xAA
	cat[32] = 0x88
	cat[64] = 0x90
	cat[65] = 0xEF
	cat[96] = 0x88
	binary.LittleEndian.PutUint16(cat[96+6:96+8], uint16(fileSize/512))
	binary.LittleEndian.PutUint32(cat[96+8:96+12], promptLBA)
}

// buildMinimalUDFWindowsISO mirrors a Windows eval ISO: ISO9660 stub (no efi),
// El Torito, UDF 1.02 with mixed-case efi/Microsoft/Boot.
func buildMinimalUDFWindowsISO() []byte {
	const (
		sectors        = 360
		catalogLBA     = 25
		partStart      = 270
		fsdRel         = 0
		rootFERel      = 1
		rootDirRel     = 2
		efiFERel       = 3
		efiDirRel      = 4
		msFERel        = 5
		msDirRel       = 6
		bootFERel      = 7
		bootDirRel     = 8
		promptFERel    = 9
		nopromptFERel  = 10
		promptDataRel  = 20
		nopromptDataRel = 24
		fileSize       = 4 * isoSectorSize
	)
	img := make([]byte, sectors*isoSectorSize)

	pvd := img[16*isoSectorSize : 17*isoSectorSize]
	pvd[0] = isoPVDType
	copy(pvd[1:], isoStdID)
	putDirRecord(pvd, 156, "\x00", 26, isoSectorSize, true)

	bootRec := img[17*isoSectorSize : 18*isoSectorSize]
	bootRec[0] = isoBootType
	copy(bootRec[1:], isoStdID)
	copy(bootRec[7:], "EL TORITO SPECIFICATION")
	binary.LittleEndian.PutUint32(bootRec[71:75], catalogLBA)

	term := img[18*isoSectorSize : 19*isoSectorSize]
	term[0] = isoTerminatorType
	copy(term[1:], isoStdID)

	putUDFVRS(img, 19, "BEA01")
	putUDFVRS(img, 20, "NSR02")
	putUDFVRS(img, 21, "TEA01")

	stub := img[26*isoSectorSize : 27*isoSectorSize]
	off := 0
	off = putDirRecord(stub, off, "\x00", 26, isoSectorSize, true)
	putDirRecord(stub, off, "\x01", 26, isoSectorSize, true)

	promptAbs := uint32(partStart + promptDataRel)
	nopromptAbs := uint32(partStart + nopromptDataRel)
	putElToritoCatalog(img[catalogLBA*isoSectorSize:(catalogLBA+1)*isoSectorSize], promptAbs, uint32(fileSize))

	avdp := img[256*isoSectorSize : 257*isoSectorSize]
	putUDFTag(avdp, udfTagAVDP, 256)
	binary.LittleEndian.PutUint32(avdp[16:20], 16*isoSectorSize)
	binary.LittleEndian.PutUint32(avdp[20:24], 257)

	pd := img[257*isoSectorSize : 258*isoSectorSize]
	putUDFTag(pd, udfTagPartition, 257)
	binary.LittleEndian.PutUint32(pd[16:20], 1)
	binary.LittleEndian.PutUint16(pd[20:22], 1)
	binary.LittleEndian.PutUint32(pd[188:192], partStart)
	binary.LittleEndian.PutUint32(pd[192:196], 80)

	lvd := img[258*isoSectorSize : 259*isoSectorSize]
	putUDFTag(lvd, udfTagLogicalVolume, 258)
	binary.LittleEndian.PutUint32(lvd[212:216], isoSectorSize)
	binary.LittleEndian.PutUint32(lvd[248:252], isoSectorSize)
	binary.LittleEndian.PutUint32(lvd[252:256], fsdRel)

	termU := img[259*isoSectorSize : 260*isoSectorSize]
	putUDFTag(termU, udfTagTerminating, 259)

	fsd := img[(partStart+fsdRel)*isoSectorSize : (partStart+fsdRel+1)*isoSectorSize]
	putUDFTag(fsd, udfTagFileSet, partStart+fsdRel)
	binary.LittleEndian.PutUint32(fsd[400:404], isoSectorSize)
	binary.LittleEndian.PutUint32(fsd[404:408], rootFERel)

	putUDFFileEntry(img, partStart, rootFERel, 4, isoSectorSize, rootDirRel)
	rootDir := img[(partStart+rootDirRel)*isoSectorSize : (partStart+rootDirRel+1)*isoSectorSize]
	putUDFFID(rootDir, 0, partStart+rootDirRel, "efi", true, efiFERel)

	putUDFFileEntry(img, partStart, efiFERel, 4, isoSectorSize, efiDirRel)
	efiDir := img[(partStart+efiDirRel)*isoSectorSize : (partStart+efiDirRel+1)*isoSectorSize]
	putUDFFID(efiDir, 0, partStart+efiDirRel, "Microsoft", true, msFERel)

	putUDFFileEntry(img, partStart, msFERel, 4, isoSectorSize, msDirRel)
	msDir := img[(partStart+msDirRel)*isoSectorSize : (partStart+msDirRel+1)*isoSectorSize]
	putUDFFID(msDir, 0, partStart+msDirRel, "Boot", true, bootFERel)

	putUDFFileEntry(img, partStart, bootFERel, 4, isoSectorSize, bootDirRel)
	bootDir := img[(partStart+bootDirRel)*isoSectorSize : (partStart+bootDirRel+1)*isoSectorSize]
	off = putUDFFID(bootDir, 0, partStart+bootDirRel, "efisys.bin", false, promptFERel)
	putUDFFID(bootDir, off, partStart+bootDirRel, "efisys_noprompt.bin", false, nopromptFERel)

	putUDFFileEntry(img, partStart, promptFERel, 5, uint32(fileSize), promptDataRel)
	putUDFFileEntry(img, partStart, nopromptFERel, 5, uint32(fileSize), nopromptDataRel)

	for i := 0; i < fileSize; i++ {
		img[int(promptAbs)*isoSectorSize+i] = 0xAA
		img[int(nopromptAbs)*isoSectorSize+i] = 0xBB
	}
	return img
}

func putUDFVRS(img []byte, lba uint32, id string) {
	sec := img[lba*isoSectorSize : (lba+1)*isoSectorSize]
	copy(sec[1:], id)
	sec[6] = 1
}

func putUDFTag(buf []byte, id, location uint32) {
	binary.LittleEndian.PutUint16(buf[0:2], uint16(id))
	binary.LittleEndian.PutUint16(buf[2:4], 2)
	binary.LittleEndian.PutUint32(buf[12:16], location)
	sum := 0
	for i := 0; i < 16; i++ {
		if i == 4 {
			continue
		}
		sum += int(buf[i])
	}
	buf[4] = byte(sum)
}

func putUDFFileEntry(img []byte, partStart, rel uint32, fileType uint8, infoLen, dataRel uint32) {
	abs := partStart + rel
	sec := img[abs*isoSectorSize : (abs+1)*isoSectorSize]
	putUDFTag(sec, udfTagFileEntry, abs)
	binary.LittleEndian.PutUint16(sec[20:22], 4)
	binary.LittleEndian.PutUint16(sec[24:26], 1)
	sec[27] = fileType
	binary.LittleEndian.PutUint16(sec[48:50], 1)
	binary.LittleEndian.PutUint64(sec[56:64], uint64(infoLen))
	nsec := (infoLen + isoSectorSize - 1) / isoSectorSize
	binary.LittleEndian.PutUint64(sec[64:72], uint64(nsec))
	binary.LittleEndian.PutUint32(sec[172:176], 8)
	binary.LittleEndian.PutUint32(sec[176:180], infoLen)
	binary.LittleEndian.PutUint32(sec[180:184], dataRel)
}

func putUDFFID(buf []byte, off int, absSector uint32, name string, isDir bool, icbRel uint32) int {
	lFI := 1 + len(name)
	recLen := 38 + lFI
	recLen = (recLen + 3) &^ 3
	rec := buf[off:]
	putUDFTag(rec, udfTagFileIdentifier, absSector)
	binary.LittleEndian.PutUint16(rec[16:18], 1)
	if isDir {
		rec[18] = 0x02
	}
	rec[19] = byte(lFI)
	binary.LittleEndian.PutUint32(rec[20:24], isoSectorSize)
	binary.LittleEndian.PutUint32(rec[24:28], icbRel)
	rec[38] = 8
	copy(rec[39:], name)
	return off + recLen
}

func putDirRecord(buf []byte, off int, name string, lba, size uint32, isDir bool) int {
	nb := []byte(name)
	recLen := 33 + len(nb)
	if recLen%2 == 1 {
		recLen++
	}
	buf[off] = byte(recLen)
	binary.LittleEndian.PutUint32(buf[off+2:off+6], lba)
	binary.BigEndian.PutUint32(buf[off+6:off+10], lba)
	binary.LittleEndian.PutUint32(buf[off+10:off+14], size)
	binary.BigEndian.PutUint32(buf[off+14:off+18], size)
	if isDir {
		buf[off+25] = 0x02
	}
	buf[off+28] = 1
	buf[off+31] = 1
	buf[off+32] = byte(len(nb))
	copy(buf[off+33:], nb)
	return off + recLen
}
