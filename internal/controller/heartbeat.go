package controller

import "slices"

// heartbeatTargets computes which controller IPs to forward heartbeats to
// and the full forwarded-for chain. It uses a single backing allocation.
func heartbeatTargets(ringIPs, forwardedFor []string, podIP string) (targets, allForwardedFor []string) {
	// Single allocation: forwardedFor entries + podIP + room for all ring IPs
	all := make([]string, 0, len(forwardedFor)+1+len(ringIPs))
	all = append(all, forwardedFor...)
	all = append(all, podIP)
	seenCount := len(all)

	for _, ip := range ringIPs {
		if !slices.Contains(all[:seenCount], ip) {
			all = append(all, ip)
		}
	}

	return all[seenCount:], all
}
