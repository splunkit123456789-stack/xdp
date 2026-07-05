package event

import "crypto/rand"

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return string([]byte{
		hex[b[0]>>4], hex[b[0]&0x0f],
		hex[b[1]>>4], hex[b[1]&0x0f],
		hex[b[2]>>4], hex[b[2]&0x0f],
		hex[b[3]>>4], hex[b[3]&0x0f],
		'-',
		hex[b[4]>>4], hex[b[4]&0x0f],
		hex[b[5]>>4], hex[b[5]&0x0f],
		'-',
		hex[b[6]>>4], hex[b[6]&0x0f],
		hex[b[7]>>4], hex[b[7]&0x0f],
		'-',
		hex[b[8]>>4], hex[b[8]&0x0f],
		hex[b[9]>>4], hex[b[9]&0x0f],
		'-',
		hex[b[10]>>4], hex[b[10]&0x0f],
		hex[b[11]>>4], hex[b[11]&0x0f],
		hex[b[12]>>4], hex[b[12]&0x0f],
		hex[b[13]>>4], hex[b[13]&0x0f],
		hex[b[14]>>4], hex[b[14]&0x0f],
		hex[b[15]>>4], hex[b[15]&0x0f],
	})
}

const hex = "0123456789abcdef"
