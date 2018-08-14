package eni

import (
	"crypto/sha512"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/node"
)

type OTAInfo struct {
	LibName  string
	Version  string   // The format of version should be vX.Y.Z where X, Y, Z are all integers. E.g. v1.0.0, v3.2.0
	Url      []string // URL to retrieve the library file
	Checksum string   // SHA512 checksum to check the health of the library
}

type OTAInstance struct {
	enableInfos    map[string]OTAInfo
	libPath        string
	stagingLibPath string
	retiredLibPath string
}

func NewOTAInstance() *OTAInstance {
	libPath, err := getLibPath()
	if err != nil {
		return nil
	}

	initializeLibDir()
	ota := OTAInstance{
		enableInfos:    make(map[string]OTAInfo),
		libPath:        libPath,
		stagingLibPath: filepath.Join(libPath, "staging"),
		retiredLibPath: filepath.Join(libPath, "retired"),
	}
	ota.loadExistedLib()
	return &ota
}

// Use OTAInfo to generate real path name of a single library
func generateFileName(info OTAInfo) string {
	fileName := info.LibName + "_" + info.Version + ".so"
	return fileName
}

type Version struct {
	major int
	minor int
	patch int
}

// Create staging and retired directories if these are not existed
func initializeLibDir() {
	libPath, err := getLibPath()
	if err != nil {
		return
	}

	os.Mkdir(filepath.Join(libPath, "staging"), 0644)
	os.Mkdir(filepath.Join(libPath, "retired"), 0644)
}

func NewVersion() *Version {
	v := Version{
		major: 0,
		minor: 0,
		patch: 0,
	}
	return &v
}

// Convert version string to a Version struct
func (v *Version) BuildFromString(version string) error {
	// Check the version format
	versionReg := regexp.MustCompile(`\Av\d+\.\d+\.\d+\z`)
	if !versionReg.MatchString(version) {
		return errors.New("The format of version is invalid")
	}

	versionSlice := strings.Split(version[1:], ".")
	v.major, _ = strconv.Atoi(versionSlice[0])
	v.minor, _ = strconv.Atoi(versionSlice[1])
	v.patch, _ = strconv.Atoi(versionSlice[2])
	return nil
}

// Compare version between two OTAInfos
// Return:
//   this > version -> 1
//   this = version -> 0
//   this < version -> -1
func (v *Version) Compare(a Version) int {
	if v.major > a.major {
		return +1
	} else if v.major < a.major {
		return -1
	} else {
		if v.minor > a.minor {
			return +1
		} else if v.minor < a.minor {
			return -1
		} else {
			if v.patch > a.patch {
				return +1
			} else if v.patch < a.patch {
				return -1
			} else {
				return 0
			}
		}
	}
}

// Check a given OTAInfo is valid to be register
func (ota *OTAInstance) IsValidNewLib(info OTAInfo) (bool, error) {
	nameReg := regexp.MustCompile(`\A[A-Za-z][A-Za-z_]*[A-Za-z]\z`)
	if !nameReg.MatchString(info.LibName) {
		return false, nil
	}
	nextVersion := NewVersion()
	err := nextVersion.BuildFromString(info.Version)
	if err != nil {
		return false, err
	}

	if _, ok := ota.enableInfos[info.LibName]; !ok {
		return true, nil
	}
	currentVersion := NewVersion()
	err = currentVersion.BuildFromString(ota.enableInfos[info.LibName].Version)
	if err != nil {
		return false, err
	}

	if nextVersion.Compare(*currentVersion) > 0 {
		return true, nil
	} else {
		return false, nil
	}
}

// Verify downloaded staging libraries.
func (ota *OTAInstance) verify(info OTAInfo) (err error) {
	libFile, err := os.Open(filepath.Join(
		ota.stagingLibPath,
		generateFileName(info)))
	if err != nil {
		return err
	}
	defer libFile.Close()

	hasher := sha512.New()
	if _, err := io.Copy(hasher, libFile); err != nil {
		return err
	}

	checksum := fmt.Sprintf("%x", hasher.Sum(nil))
	if checksum != info.checksum {
		os.Remove(filepath.Join(
			ota.stagingLibPath,
			generateFileName(info)))
		return errors.New("Library " + info.libName + " checksum doesn't match")
	}
	return nil
}

// Register staging libraries to lib.
func (ota *OTAInstance) Register(info OTAInfo) (err error) {
	// If there is an old version, move it to retired folder.
	if originInfo, exist := ota.enableInfos[info.LibName]; exist {
		err = os.Rename(
			filepath.Join(ota.libPath, generateFileName(originInfo)),
			filepath.Join(ota.retiredLibPath, generateFileName(originInfo)))
		if err != nil {
			return err
		}
		// Remove the retired library from enableInfos
		delete(ota.enableInfos, info.LibName)
	}

	// Overwrite old libraries by libName.
	ota.enableInfos[info.libName] = info
	err = os.Rename(
		filepath.Join(ota.stagingLibPath, generateFileName(info)),
		filepath.Join(ota.libPath, generateFileName(info)))

	if err != nil {
		return err
	}

	return nil
}

// Remove unused libraries from lib, staging, and retired folder
func (ota *OTAInstance) Destroy(info OTAInfo) (err error) {
	err = removeLibrary(ota.libPath, info)
	if err != nil {
		return err
	}
	err = removeLibrary(ota.stagingLibPath, info)
	if err != nil {
		return err
	}
	err = removeLibrary(ota.retiredLibPath, info)
	if err != nil {
		return err
	}
	return nil
}

// Remove library from specific path
func removeLibrary(path string, info OTAInfo) (err error) {
	fileName := filepath.Join(path, generateFileName(info))
	if _, err := os.Stat(fileName); err == nil {
		err = os.Remove(fileName)
		if err != nil {
			return err
		}
	}
	return nil
}

// Get libPath from default data path or ENI_LIBRARY_PATH
func getLibPath() (libPath string, err error) {
	libPath = filepath.Join(node.DefaultDataDir(), "eni", "lib")
	if val, ok := os.LookupEnv("ENI_LIBRARY_PATH"); ok {
		libPath = val
	}

	// Check if the ENI_LIBRARY_PATH is existed.
	fileinfo, err := os.Stat(libPath)
	if err != nil {
		return "", err
	}
	if !fileinfo.Mode().IsDir() {
		return "", errors.New("can't find dynamic library path: " + libPath)
	}
	return libPath, nil
}

// Download the library from given url. The library will
// be saved in ENI_LIBRARY_PATH/staging named OTAInfo.LibName.
func (ota *OTAInstance) DownloadInfo(info OTAInfo) (err error) {
	// If file is existed, we don't need to download it again.
	fileName := filepath.Join(ota.stagingLibPath, generateFileName(info))
	if _, err := os.Stat(fileName); err == nil {
		if err = ota.verify(info); err == nil {
			return nil
		}
	}

	for _, url := range info.Url {
		err = ota.downloadFromUrl(fileName, url)
		if err == nil {
			err = ota.verify(info)
			if err == nil {
				break
			}
		}
	}

	return
}

func (ota *OTAInstance) downloadFromUrl(fileName, url string) (err error) {
	output, err := os.Create(fileName)
	if err != nil {
		return
	}
	defer output.Close()

	response, err := http.Get(url)
	if err != nil {
		os.Remove(fileName)
		return
	}
	defer response.Body.Close()

	_, err = io.Copy(output, response.Body)
	if err != nil {
		os.Remove(fileName)
		return
	}

	return nil
}

// Load existed libraries from ENI library path.
func (ota *OTAInstance) loadExistedLib() {
	var dynamicLibs []string

	// Get all dynamic libraries from library path.
	filepath.Walk(ota.libPath, func(path string, fileinfo os.FileInfo, err error) error {
		if fileinfo.IsDir() && (path == ota.retiredLibPath || path == ota.stagingLibPath) {
			return filepath.SkipDir
		}
		if fileinfo.Mode().IsRegular() && filepath.Ext(path) == ".so" {
			dynamicLibs = append(dynamicLibs, fileinfo.Name())
		}
		return nil
	})

	// Library name format examples:
	//	reverse_v1.0.0.so
	//	eni_rsa_v1.5.9.so
	//	eni_scrypt_v9.10.11.so
	libReg := regexp.MustCompile(`\A[A-Za-z][A-Za-z_]*[A-Za-z]_v\d+\.\d+\.\d+\.so\z`)
	for _, lib := range dynamicLibs {
		if libReg.MatchString(lib) {
			// Parse LibName and Version from the file name.
			libSlice := strings.Split(lib, "_")
			libName := strings.Join(libSlice[:len(libSlice)-1], "_")
			versionSlice := strings.Split(libSlice[len(libSlice)-1], ".")
			version := strings.Join(versionSlice[:len(versionSlice)-1], ".")
			info := OTAInfo{
				LibName:  libName,
				Version:  version,
				Url:      []string{},
				Checksum: "",
			}
			// Load local libraries into enable info list.
			ota.enableInfos[info.LibName] = info
		}
	}
}
