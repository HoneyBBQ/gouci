package gouci

import (
	"bytes"
	"io"

	"github.com/honeybbq/gouci/internal/errs"
)

// Format serializes p into clean canonical /etc/config/<pkg> format (without a
// package header), using single-quoted values escaped with '\”.
func (p *Package) Format() ([]byte, error) {
	var buf bytes.Buffer
	err := p.formatSections(&buf)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Export serializes p in "uci -N export" format with a leading "package <name>"
// header. It returns ErrInvalidName if p.Name() is empty or invalid.
func (p *Package) Export() ([]byte, error) {
	if !validatePackageName(p.name) {
		return nil, errs.Wrapf(ErrInvalidName, "invalid package name %q", p.name)
	}
	var buf bytes.Buffer
	buf.WriteString("package ")
	writeEscaped(&buf, p.name)
	buf.WriteByte('\n')
	if len(p.sections) > 0 {
		buf.WriteByte('\n')
		err := p.formatSections(&buf)
		if err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

// WriteTo writes p's canonical /etc/config/<pkg> representation to w.
func (p *Package) WriteTo(w io.Writer) (int64, error) {
	data, err := p.Format()
	if err != nil {
		return 0, err
	}
	n, err := w.Write(data)

	return int64(n), err
}

func (p *Package) formatSections(buf *bytes.Buffer) error {
	for i, s := range p.sections {
		if i > 0 {
			buf.WriteByte('\n')
		}
		err := formatSection(buf, s)
		if err != nil {
			return err
		}
	}

	return nil
}

func formatSection(buf *bytes.Buffer, s *Section) error {
	if !validateType(s.typ) {
		return errs.Wrapf(ErrInvalidName, "invalid section type %q", s.typ)
	}
	buf.WriteString("config ")
	writeType(buf, s.typ)
	if !s.anonymous {
		if !validateName(s.name) {
			return errs.Wrapf(ErrInvalidName, "invalid section name %q", s.name)
		}
		buf.WriteString(" '")
		writeEscaped(buf, s.name)
		buf.WriteByte('\'')
	}
	buf.WriteByte('\n')

	for _, e := range s.entries {
		err := formatEntry(buf, e)
		if err != nil {
			return err
		}
	}

	return nil
}

func writeType(buf *bytes.Buffer, typ string) {
	if typeNeedsQuoting(typ) {
		buf.WriteByte('\'')
		writeEscaped(buf, typ)
		buf.WriteByte('\'')

		return
	}
	buf.WriteString(typ)
}

func typeNeedsQuoting(typ string) bool {
	for i := range len(typ) {
		switch typ[i] {
		case '\'', '"', '\\', '#', ';':
			return true
		}
	}

	return false
}

func formatEntry(buf *bytes.Buffer, e Entry) error {
	if !validateName(e.Key) {
		return errs.Wrapf(ErrInvalidName, "invalid option name %q", e.Key)
	}
	switch e.Kind {
	case KindScalar:
		buf.WriteString("\toption ")
		buf.WriteString(e.Key)
		buf.WriteString(" '")
		writeEscaped(buf, e.Value)
		buf.WriteString("'\n")
	case KindList:
		for _, v := range e.Values {
			buf.WriteString("\tlist ")
			buf.WriteString(e.Key)
			buf.WriteString(" '")
			writeEscaped(buf, v)
			buf.WriteString("'\n")
		}
	default:
		return errs.Wrapf(ErrInvalidValue, "unknown entry kind %d for %q", e.Kind, e.Key)
	}

	return nil
}

func writeEscaped(buf *bytes.Buffer, s string) {
	for i := range len(s) {
		if s[i] == '\'' {
			buf.WriteString(`'\''`)

			continue
		}
		buf.WriteByte(s[i])
	}
}
