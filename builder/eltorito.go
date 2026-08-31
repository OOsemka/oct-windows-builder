package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

const (
	isoSectorSize      = 2048
	isoPVDType         = 1
	isoBootType        = 0
	isoSVDType         = 2
	isoTerminatorType  = 255
	isoStdID           = "CD001"
	eltoritoEFISection = 0xEF
	eltoritoBootable   = 0x88
)

type isoReadWriter interface {
	io.Reader
	io.Writer
	io.Seeker
	io.ReaderAt
}

type isoExtent struct {
	name string
	lba  uint32
	size uint32
}

// patchISONoprompt rewrites a Windows install ISO so UEFI El Torito boots
// without "Press any key to boot from CD or DVD". Microsoft ships both
// efi/microsoft/boot/efisys.bin (prompt) and efisys_noprompt.bin (no prompt);
// kubevirt-tekton-tasks windows-efi-installer remasters the ISO. We do the same
// in place: overwrite the prompt boot image with the noprompt bytes (same size)
// and retarget the El Torito EFI catalog entry.
func patchISONoprompt(root string) error {
	path, err := resolveISOPath(root)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open ISO: %w", err)
	}
	defer f.Close()
	if err := patchElToritoNoprompt(f); err != nil {
		return err
	}
	_ = os.Chmod(path, 0644)
	logf("El Torito noprompt patch applied path=%s", filepath.Base(path))
	return nil
}

func resolveISOPath(root string) (string, error) {
	st, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("ISO path: %w", err)
	}
	if !st.IsDir() {
		return root, nil
	}
	names := []string{"disk.img", "disk.iso", "disk.raw"}
	for _, n := range names {
		p := filepath.Join(root, n)
		if fi, err := os.Stat(p); err == nil && fi.Size() > isoSectorSize {
			return p, nil
		}
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		if fi, err := e.Info(); err == nil && fi.Size() > 1024*1024 {
			return p, nil
		}
	}
	return "", fmt.Errorf("no disk.img in ISO volume %s", root)
}

func patchElToritoNoprompt(rw isoReadWriter) error {
	prompt, noprompt, catalogLBA, err := inspectWindowsISO(rw)
	if err != nil {
		return err
	}
	if noprompt.lba == 0 || noprompt.size == 0 {
		return fmt.Errorf("ISO is missing efi/microsoft/boot/efisys_noprompt.bin (needed for unattended UEFI)")
	}
	same, err := extentsEqual(rw, prompt, noprompt)
	if err != nil {
		return err
	}
	if prompt.lba != 0 && prompt.size == noprompt.size && !same {
		if err := copyExtent(rw, noprompt, prompt); err != nil {
			return err
		}
		logf("overwrote El Torito prompt boot image with noprompt (%d bytes)", noprompt.size)
	} else if same {
		logf("ISO prompt boot image already matches noprompt")
	} else if prompt.lba != 0 && prompt.size != noprompt.size {
		logf("prompt (%d) and noprompt (%d) sizes differ; catalog retarget only", prompt.size, noprompt.size)
	}
	if catalogLBA == 0 {
		if prompt.lba != 0 && prompt.size == noprompt.size {
			return nil
		}
		return fmt.Errorf("ISO has no El Torito boot catalog")
	}
	return retargetElToritoEFI(rw, catalogLBA, noprompt.lba, noprompt.size)
}

func inspectWindowsISO(r io.ReaderAt) (prompt, noprompt isoExtent, catalogLBA uint32, err error) {
	var pvdRoot, jolietRoot isoExtent
	for lba := uint32(16); lba < 32; lba++ {
		sec, e := readSectorAt(r, lba)
		if e != nil {
			return prompt, noprompt, 0, e
		}
		if string(sec[1:6]) != isoStdID {
			continue
		}
		switch sec[0] {
		case isoPVDType:
			pvdRoot = dirRecordAt(sec, 156)
		case isoSVDType:
			if sec[88] == 0x25 && sec[89] == 0x2F && (sec[90] == 0x45 || sec[90] == 0x43 || sec[90] == 0x40) {
				jolietRoot = dirRecordAt(sec, 156)
			}
		case isoBootType:
			id := strings.TrimRight(string(sec[7:39]), " \x00")
			if strings.HasPrefix(id, "EL TORITO") {
				catalogLBA = binary.LittleEndian.Uint32(sec[71:75])
			}
		case isoTerminatorType:
			break
		}
	}
	path := []string{"efi", "microsoft", "boot"}
	files := []isoExtent{}
	if jolietRoot.lba != 0 {
		files, err = listDirPath(r, jolietRoot, path, true)
		if err != nil {
			logf("Joliet walk: %v", err)
			files = nil
			err = nil
		}
	}
	if len(files) == 0 && pvdRoot.lba != 0 {
		files, err = listDirPath(r, pvdRoot, path, false)
		if err != nil {
			return prompt, noprompt, catalogLBA, fmt.Errorf("ISO9660 walk: %w", err)
		}
	}
	for _, f := range files {
		n := strings.ToLower(f.name)
		switch n {
		case "efisys.bin":
			prompt = f
		case "efisys_noprompt.bin", "efisys_n.bin":
			noprompt = f
		}
	}
	if noprompt.lba == 0 {
		return prompt, noprompt, catalogLBA, fmt.Errorf("efi/microsoft/boot/efisys_noprompt.bin not found (Joliet lba=%d ISO lba=%d)", jolietRoot.lba, pvdRoot.lba)
	}
	return prompt, noprompt, catalogLBA, nil
}

func listDirPath(r io.ReaderAt, root isoExtent, parts []string, joliet bool) ([]isoExtent, error) {
	cur := root
	for _, p := range parts {
		ents, err := readDirectory(r, cur, joliet)
		if err != nil {
			return nil, err
		}
		var next *isoExtent
		for i := range ents {
			if ents[i].name == "" {
				continue
			}
			if strings.EqualFold(ents[i].name, p) {
				next = &ents[i]
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("missing directory %q", p)
		}
		cur = *next
	}
	return readDirectory(r, cur, joliet)
}

func readDirectory(r io.ReaderAt, dir isoExtent, joliet bool) ([]isoExtent, error) {
	if dir.size == 0 {
		return nil, fmt.Errorf("empty directory")
	}
	buf := make([]byte, dir.size)
	if _, err := r.ReadAt(buf, int64(dir.lba)*isoSectorSize); err != nil && err != io.EOF {
		return nil, err
	}
	var out []isoExtent
	for off := 0; off < len(buf); {
		if off%isoSectorSize == 0 && buf[off] == 0 {
			off += isoSectorSize - (off % isoSectorSize)
			if off >= len(buf) {
				break
			}
		}
		recLen := int(buf[off])
		if recLen == 0 {
			next := ((off / isoSectorSize) + 1) * isoSectorSize
			if next <= off {
				break
			}
			off = next
			continue
		}
		if off+recLen > len(buf) {
			break
		}
		rec := buf[off : off+recLen]
		off += recLen
		nameLen := int(rec[32])
		if nameLen == 0 || 33+nameLen > len(rec) {
			continue
		}
		raw := rec[33 : 33+nameLen]
		if nameLen == 1 && (raw[0] == 0 || raw[0] == 1) {
			continue
		}
		name := decodeISOName(raw, joliet)
		if name == "" {
			continue
		}
		out = append(out, isoExtent{
			name: name,
			lba:  binary.LittleEndian.Uint32(rec[2:6]),
			size: binary.LittleEndian.Uint32(rec[10:14]),
		})
	}
	return out, nil
}

func decodeISOName(raw []byte, joliet bool) string {
	var s string
	if joliet {
		if len(raw)%2 == 1 {
			raw = raw[:len(raw)-1]
		}
		u16 := make([]uint16, len(raw)/2)
		for i := range u16 {
			u16[i] = binary.BigEndian.Uint16(raw[i*2:])
		}
		s = string(utf16.Decode(u16))
	} else {
		s = string(raw)
	}
	s = strings.TrimRight(s, " ")
	if i := strings.Index(s, ";"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimRight(s, ".")
	return s
}

func dirRecordAt(sec []byte, off int) isoExtent {
	if off+14 > len(sec) || sec[off] == 0 {
		return isoExtent{}
	}
	return isoExtent{
		lba:  binary.LittleEndian.Uint32(sec[off+2 : off+6]),
		size: binary.LittleEndian.Uint32(sec[off+10 : off+14]),
	}
}

func readSectorAt(r io.ReaderAt, lba uint32) ([]byte, error) {
	buf := make([]byte, isoSectorSize)
	n, err := r.ReadAt(buf, int64(lba)*isoSectorSize)
	if n != isoSectorSize {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("short ISO sector %d", lba)
	}
	return buf, nil
}

func extentsEqual(r io.ReaderAt, a, b isoExtent) (bool, error) {
	if a.lba == 0 || b.lba == 0 || a.size != b.size {
		return false, nil
	}
	if a.lba == b.lba {
		return true, nil
	}
	bufA := make([]byte, a.size)
	bufB := make([]byte, b.size)
	if _, err := r.ReadAt(bufA, int64(a.lba)*isoSectorSize); err != nil && err != io.EOF {
		return false, err
	}
	if _, err := r.ReadAt(bufB, int64(b.lba)*isoSectorSize); err != nil && err != io.EOF {
		return false, err
	}
	return string(bufA) == string(bufB), nil
}

func copyExtent(rw io.ReadWriteSeeker, src, dst isoExtent) error {
	buf := make([]byte, src.size)
	if _, err := rw.Seek(int64(src.lba)*isoSectorSize, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.ReadFull(rw, buf); err != nil {
		return fmt.Errorf("read noprompt boot image: %w", err)
	}
	if _, err := rw.Seek(int64(dst.lba)*isoSectorSize, io.SeekStart); err != nil {
		return err
	}
	if _, err := rw.Write(buf); err != nil {
		return fmt.Errorf("write noprompt boot image: %w", err)
	}
	return nil
}

func retargetElToritoEFI(rw io.ReadWriteSeeker, catalogLBA, nopromptLBA, nopromptSize uint32) error {
	if _, err := rw.Seek(int64(catalogLBA)*isoSectorSize, io.SeekStart); err != nil {
		return err
	}
	cat := make([]byte, isoSectorSize)
	if _, err := io.ReadFull(rw, cat); err != nil {
		return fmt.Errorf("read El Torito catalog: %w", err)
	}
	patched := 0
	inEFI := false
	secCount := uint16((nopromptSize + 511) / 512)
	if secCount == 0 {
		secCount = 1
	}
	for off := 0; off+32 <= len(cat); off += 32 {
		id := cat[off]
		switch {
		case id == 0x01:
			inEFI = false
		case id == 0x90 || id == 0x91:
			inEFI = cat[off+1] == eltoritoEFISection
		case id == eltoritoBootable && inEFI:
			binary.LittleEndian.PutUint32(cat[off+8:off+12], nopromptLBA)
			binary.LittleEndian.PutUint16(cat[off+6:off+8], secCount)
			patched++
		case id == 0:
			off = len(cat)
		default:
			inEFI = false
		}
	}
	if patched == 0 {
		logf("no EFI El Torito section entry to retarget (catalog lba=%d)", catalogLBA)
		return nil
	}
	if _, err := rw.Seek(int64(catalogLBA)*isoSectorSize, io.SeekStart); err != nil {
		return err
	}
	if _, err := rw.Write(cat); err != nil {
		return fmt.Errorf("write El Torito catalog: %w", err)
	}
	logf("retargeted %d EFI El Torito catalog entries to noprompt LBA %d", patched, nopromptLBA)
	return nil
}
