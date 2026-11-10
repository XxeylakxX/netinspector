//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// hideWindow stops child processes from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

var (
	modwlanapi                = syscall.NewLazyDLL("wlanapi.dll")
	procWlanOpenHandle        = modwlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle       = modwlanapi.NewProc("WlanCloseHandle")
	procWlanEnumInterfaces    = modwlanapi.NewProc("WlanEnumInterfaces")
	procWlanGetNetworkBssList = modwlanapi.NewProc("WlanGetNetworkBssList")
	procWlanFreeMemory        = modwlanapi.NewProc("WlanFreeMemory")
)

type wlanGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type wlanInterfaceInfo struct {
	InterfaceGuid            wlanGUID
	strInterfaceDescription [256]uint16
	isState                  uint32
}

type wlanInterfaceInfoList struct {
	dwNumberOfItems uint32
	dwIndex         uint32
	InterfaceInfo   [1]wlanInterfaceInfo
}

// queryChannelWidth retrieves the channel bandwidth (e.g. "20 MHz", "40 MHz", "80 MHz", "160 MHz", "320 MHz")
// for the connected Wi-Fi interface using Windows Native Wifi API (WlanGetNetworkBssList) and beacon IEs.
func queryChannelWidth(targetBssid string) string {
	if procWlanOpenHandle.Find() != nil {
		return ""
	}

	var clientHandle uintptr
	var negotiatedVersion uint32

	ret, _, _ := procWlanOpenHandle.Call(
		2,
		0,
		uintptr(unsafe.Pointer(&negotiatedVersion)),
		uintptr(unsafe.Pointer(&clientHandle)),
	)
	if ret != 0 {
		return ""
	}
	defer procWlanCloseHandle.Call(clientHandle, 0)

	var pIfList uintptr
	ret, _, _ = procWlanEnumInterfaces.Call(clientHandle, 0, uintptr(unsafe.Pointer(&pIfList)))
	if ret != 0 || pIfList == 0 {
		return ""
	}
	defer procWlanFreeMemory.Call(pIfList)

	ifList := (*wlanInterfaceInfoList)(unsafe.Pointer(pIfList))
	if ifList.dwNumberOfItems == 0 {
		return ""
	}

	normTarget := strings.ToLower(strings.ReplaceAll(targetBssid, "-", ":"))

	for i := uint32(0); i < ifList.dwNumberOfItems; i++ {
		info := (*wlanInterfaceInfo)(unsafe.Pointer(pIfList + 8 + uintptr(i)*532))
		guid := info.InterfaceGuid

		var pBssList uintptr
		ret, _, _ = procWlanGetNetworkBssList.Call(
			clientHandle,
			uintptr(unsafe.Pointer(&guid)),
			0,
			3, // dot11_BSS_type_any
			0,
			0,
			uintptr(unsafe.Pointer(&pBssList)),
		)
		if ret != 0 || pBssList == 0 {
			continue
		}

		totalSize := *(*uint32)(unsafe.Pointer(pBssList))
		numItems := *(*uint32)(unsafe.Pointer(pBssList + 4))

		currEntryPtr := pBssList + 8
		listEnd := pBssList + uintptr(totalSize)

		var fallbackWidth string

		for j := uint32(0); j < numItems && currEntryPtr < listEnd; j++ {
			entrySlice := unsafe.Slice((*byte)(unsafe.Pointer(currEntryPtr)), 0x168)
			bssidBytes := entrySlice[0x28 : 0x28+6]
			bssidStr := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
				bssidBytes[0], bssidBytes[1], bssidBytes[2], bssidBytes[3], bssidBytes[4], bssidBytes[5])

			ulIeOffset := binary.LittleEndian.Uint32(entrySlice[0x160 : 0x164])
			ulIeSize := binary.LittleEndian.Uint32(entrySlice[0x164 : 0x168])

			match := (normTarget != "" && normTarget == bssidStr)
			if ulIeOffset > 0 && ulIeSize > 0 && currEntryPtr+uintptr(ulIeOffset+ulIeSize) <= listEnd {
				fullEntry := unsafe.Slice((*byte)(unsafe.Pointer(currEntryPtr)), uintptr(ulIeOffset+ulIeSize))
				ies := fullEntry[ulIeOffset : ulIeOffset+ulIeSize]
				width := parseChannelWidthFromIEs(ies)
				if match {
					procWlanFreeMemory.Call(pBssList)
					return width
				}
				if fallbackWidth == "" {
					fallbackWidth = width
				}
			}

			nextOffset := uintptr(ulIeOffset + ulIeSize)
			if nextOffset%8 != 0 {
				nextOffset += (8 - (nextOffset % 8))
			}
			if nextOffset < 0x168 {
				break
			}
			currEntryPtr += nextOffset
		}

		procWlanFreeMemory.Call(pBssList)
		if fallbackWidth != "" && normTarget == "" {
			return fallbackWidth
		}
	}

	return ""
}

// parseChannelWidthFromIEs inspects beacon Information Elements (HT, VHT, HE, EHT)
// to accurately determine the active radio channel bandwidth.
func parseChannelWidthFromIEs(ies []byte) string {
	width := "20 MHz"
	idx := 0
	for idx+2 <= len(ies) {
		id := ies[idx]
		l := int(ies[idx+1])
		if idx+2+l > len(ies) {
			break
		}
		data := ies[idx+2 : idx+2+l]

		// HT Operation (Element ID 61 - 802.11n)
		if id == 61 && len(data) >= 2 {
			secOffset := data[1] & 0x03
			if secOffset == 1 || secOffset == 3 {
				width = "40 MHz"
			}
		}

		// VHT Operation (Element ID 192 - 802.11ac)
		if id == 192 && len(data) >= 1 {
			vhtWidth := data[0]
			if vhtWidth == 1 {
				width = "80 MHz"
			} else if vhtWidth == 2 || vhtWidth == 3 {
				width = "160 MHz"
			}
		}

		// Extension Elements (Element ID 255)
		if id == 255 && len(data) >= 2 {
			extId := data[0]
			extData := data[1:]

			// HE Operation (Element ID 255, Ext ID 36 - 802.11ax / Wi-Fi 6)
			if extId == 36 && len(extData) >= 6 {
				paramBytes := extData[0:3]
				vhtPresent := (paramBytes[1] & 0x40) != 0 // Bit 14
				offset := 6
				if vhtPresent && len(extData) >= offset+3 {
					vhtWidth := extData[offset]
					if vhtWidth == 1 {
						width = "80 MHz"
					} else if vhtWidth == 2 || vhtWidth == 3 {
						width = "160 MHz"
					}
				}
				he6GHzPresent := (paramBytes[1] & 0x80) != 0 // Bit 15
				if he6GHzPresent {
					off6GHz := offset
					if vhtPresent {
						off6GHz += 3
					}
					if len(extData) >= off6GHz+5 {
						chanWidth6GHz := extData[off6GHz+1] & 0x03
						switch chanWidth6GHz {
						case 0:
							width = "20 MHz"
						case 1:
							width = "40 MHz"
						case 2:
							width = "80 MHz"
						case 3:
							width = "160 MHz"
						}
					}
				}
			}

			// EHT Operation (Element ID 255, Ext ID 106 - 802.11be / Wi-Fi 7)
			if extId == 106 && len(extData) >= 2 {
				// Bit 0 of Byte 0 indicates EHT Operation Information Present
				if (extData[0] & 0x01) != 0 {
					ehtWidth := (extData[1] >> 5) & 0x07
					if ehtWidth == 1 {
						width = "320 MHz"
					}
				}
			}
		}

		idx += 2 + l
	}
	return width
}
