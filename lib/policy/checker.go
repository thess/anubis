package policy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"

	"github.com/TecharoHQ/anubis/internal"
	"github.com/yl2chen/cidranger"
)

var (
	ErrMisconfiguration = errors.New("[unexpected] policy: administrator misconfiguration")
)

type Checker interface {
	Check(r *http.Request, optional ...bool) (bool, error)
	Hash() string
}

type CheckerList []Checker

func (cl CheckerList) Check(r *http.Request, optional ...bool) (bool, error) {
	ct := len(cl)
	nMatch := 0
	matchAll := false

	// optional is expected to be a single boolean value if supplied
	if len(optional) > 0 && optional[0] {
		matchAll = optional[0]
	}

	for _, c := range cl {
		ok, err := c.Check(r)
		if err != nil {
			return ok, err
		}
		if ok {
			if matchAll {
				nMatch++
			} else {
				return ok, nil
			}
		}
	}
	// Note: ct is never zero here, because CheckerList is always initialized with at least one Checker.
	// If it were empty, the Check method would not be called.
	// If matchAll is true, we return true only if all checkers matched.
	return nMatch == ct, nil
}

func (cl CheckerList) Hash() string {
	var sb strings.Builder

	for _, c := range cl {
		fmt.Fprintln(&sb, c.Hash())
	}

	return internal.SHA256sum(sb.String())
}

type staticHashChecker struct {
	hash string
}

func (staticHashChecker) Check(r *http.Request) (bool, error) {
	return true, nil
}

func (s staticHashChecker) Hash() string { return s.hash }

func NewStaticHashChecker(hashable string) Checker {
	return staticHashChecker{hash: internal.SHA256sum(hashable)}
}

type RemoteAddrChecker struct {
	ranger cidranger.Ranger
	hash   string
}

func NewRemoteAddrChecker(cidrs []string) (Checker, error) {
	ranger := cidranger.NewPCTrieRanger()
	var sb strings.Builder

	for _, cidr := range cidrs {
		_, rng, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("%w: range %s not parsing: %w", ErrMisconfiguration, cidr, err)
		}

		err = ranger.Insert(cidranger.NewBasicRangerEntry(*rng))
		if err != nil {
			return nil, fmt.Errorf("%w: error inserting ip range: %w", ErrMisconfiguration, err)
		}
		fmt.Fprintln(&sb, cidr)
	}

	return &RemoteAddrChecker{
		ranger: ranger,
		hash:   internal.SHA256sum(sb.String()),
	}, nil
}

func (rac *RemoteAddrChecker) Check(r *http.Request, optional ...bool) (bool, error) {
	host := r.Header.Get("X-Real-Ip")
	if host == "" {
		return false, fmt.Errorf("%w: header X-Real-Ip is not set", ErrMisconfiguration)
	}

	addr := net.ParseIP(host)
	if addr == nil {
		return false, fmt.Errorf("%w: %s is not an IP address", ErrMisconfiguration, host)
	}

	ok, err := rac.ranger.Contains(addr)
	if err != nil {
		return false, err
	}

	if ok {
		return true, nil
	}

	return false, nil
}

func (rac *RemoteAddrChecker) Hash() string {
	return rac.hash
}

type HeaderMatchesChecker struct {
	header string
	regexp *regexp.Regexp
	hash   string
}

func NewUserAgentChecker(rexStr string) (Checker, error) {
	return NewHeaderMatchesChecker("User-Agent", rexStr)
}

func NewHeaderMatchesChecker(header, rexStr string) (Checker, error) {
	rex, err := regexp.Compile(strings.TrimSpace(rexStr))
	if err != nil {
		return nil, fmt.Errorf("%w: regex %s failed parse: %w", ErrMisconfiguration, rexStr, err)
	}
	return &HeaderMatchesChecker{strings.TrimSpace(header), rex, internal.SHA256sum(header + ": " + rexStr)}, nil
}

func (hmc *HeaderMatchesChecker) Check(r *http.Request, optional ...bool) (bool, error) {
	if hmc.regexp.MatchString(r.Header.Get(hmc.header)) {
		return true, nil
	}

	return false, nil
}

func (hmc *HeaderMatchesChecker) Hash() string {
	return hmc.hash
}

type PathChecker struct {
	regexp *regexp.Regexp
	hash   string
}

func NewPathChecker(rexStr string) (Checker, error) {
	rex, err := regexp.Compile(strings.TrimSpace(rexStr))
	if err != nil {
		return nil, fmt.Errorf("%w: regex %s failed parse: %w", ErrMisconfiguration, rexStr, err)
	}
	return &PathChecker{rex, internal.SHA256sum(rexStr)}, nil
}

func (pc *PathChecker) Check(r *http.Request, optional ...bool) (bool, error) {
	if pc.regexp.MatchString(r.URL.Path) {
		return true, nil
	}

	return false, nil
}

func (pc *PathChecker) Hash() string {
	return pc.hash
}

func NewHeaderExistsChecker(key string) Checker {
	return headerExistsChecker{strings.TrimSpace(key)}
}

type headerExistsChecker struct {
	header string
}

func (hec headerExistsChecker) Check(r *http.Request, optional ...bool) (bool, error) {
	if r.Header.Get(hec.header) != "" {
		return true, nil
	}

	return false, nil
}

func (hec headerExistsChecker) Hash() string {
	return internal.SHA256sum(hec.header)
}

func NewHeadersChecker(headermap map[string]string) (Checker, error) {
	var result CheckerList
	var errs []error

	for key, rexStr := range headermap {
		if rexStr == ".*" {
			result = append(result, headerExistsChecker{strings.TrimSpace(key)})
			continue
		}

		rex, err := regexp.Compile(strings.TrimSpace(rexStr))
		if err != nil {
			errs = append(errs, fmt.Errorf("while compiling header %s regex %s: %w", key, rexStr, err))
			continue
		}

		result = append(result, &HeaderMatchesChecker{key, rex, internal.SHA256sum(key + ": " + rexStr)})
	}

	if len(errs) != 0 {
		return nil, errors.Join(errs...)
	}

	return result, nil
}
