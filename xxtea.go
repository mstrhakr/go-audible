package audible

// XXTEA implements the Corrected Block TEA (XXTEA) cipher.
// This is required for Audible metadata encryption/decryption.
//
// Reference: https://en.wikipedia.org/wiki/XXTEA

const (
	// XXTEA delta constant
	delta = 0x9E3779B9
)

// XXTEAEncrypt encrypts data using XXTEA.
// Data length must be a multiple of 4 bytes.
func XXTEAEncrypt(data, key []byte) ([]byte, error) {
	if len(data)%4 != 0 {
		return nil, ErrInvalidDataLength
	}
	if len(key) != 16 {
		return nil, ErrInvalidKeyLength
	}

	v := bytesToUint32s(data)
	k := bytesToUint32s(key)

	n := len(v)
	if n < 2 {
		return nil, ErrDataTooShort
	}

	// Encryption rounds
	q := 6 + 52/n
	var sum uint32 = 0

	for i := 0; i < q; i++ {
		sum += delta
		e := (sum >> 2) & 3
		for p := 0; p < n; p++ {
			y := v[(p+1)%n]
			var z uint32
			if p == 0 {
				z = v[n-1]
			} else {
				z = v[p-1]
			}
			v[p] += mx(sum, y, z, p, e, k)
		}
	}

	return uint32sToBytes(v), nil
}

// XXTEADecrypt decrypts data encrypted with XXTEA.
// Data length must be a multiple of 4 bytes.
func XXTEADecrypt(data, key []byte) ([]byte, error) {
	if len(data)%4 != 0 {
		return nil, ErrInvalidDataLength
	}
	if len(key) != 16 {
		return nil, ErrInvalidKeyLength
	}

	v := bytesToUint32s(data)
	k := bytesToUint32s(key)

	n := len(v)
	if n < 2 {
		return nil, ErrDataTooShort
	}

	// Decryption rounds
	q := 6 + 52/n
	sum := uint32(q) * delta

	for i := 0; i < q; i++ {
		e := (sum >> 2) & 3
		for p := n - 1; p >= 0; p-- {
			z := v[(p+n-1)%n]
			var y uint32
			if p == n-1 {
				y = v[0]
			} else {
				y = v[p+1]
			}
			v[p] -= mx(sum, y, z, p, e, k)
		}
		sum -= delta
	}

	return uint32sToBytes(v), nil
}

// mx is the XXTEA mixing function.
func mx(sum, y, z uint32, p int, e uint32, k []uint32) uint32 {
	return ((z>>5 ^ y<<2) + (y>>3 ^ z<<4)) ^ ((sum ^ y) + (k[(uint32(p)&3)^e] ^ z))
}

// bytesToUint32s converts a byte slice to a uint32 slice (little-endian).
func bytesToUint32s(b []byte) []uint32 {
	result := make([]uint32, len(b)/4)
	for i := range result {
		result[i] = uint32(b[i*4]) |
			uint32(b[i*4+1])<<8 |
			uint32(b[i*4+2])<<16 |
			uint32(b[i*4+3])<<24
	}
	return result
}

// uint32sToBytes converts a uint32 slice to a byte slice (little-endian).
func uint32sToBytes(v []uint32) []byte {
	result := make([]byte, len(v)*4)
	for i, val := range v {
		result[i*4] = byte(val)
		result[i*4+1] = byte(val >> 8)
		result[i*4+2] = byte(val >> 16)
		result[i*4+3] = byte(val >> 24)
	}
	return result
}

// Audible XXTEA key (hardcoded in the protocol)
var audibleXXTEAKey = []byte{
	0x72, 0x38, 0x33, 0xB5, 0x4E, 0x06, 0xF7, 0x03,
	0x4E, 0x28, 0x09, 0x47, 0x70, 0x84, 0x02, 0x03,
}

// DecryptAudibleMetadata decrypts Audible metadata using the hardcoded XXTEA key.
func DecryptAudibleMetadata(data []byte) ([]byte, error) {
	return XXTEADecrypt(data, audibleXXTEAKey)
}

// EncryptAudibleMetadata encrypts data using the hardcoded Audible XXTEA key.
func EncryptAudibleMetadata(data []byte) ([]byte, error) {
	return XXTEAEncrypt(data, audibleXXTEAKey)
}
