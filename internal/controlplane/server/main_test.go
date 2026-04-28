package server

// L-14 follow-up: the login timing floor is a per-Server field now;
// tests that construct a Server pass Options{LoginTimingFloor: -1} to
// skip the production 150ms pad. No package-level override is needed,
// so this file no longer carries a TestMain.
