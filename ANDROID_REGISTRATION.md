# Android device registration

Call `GetAuthURLForDevice(audible.Android)` and pass `Profile: audible.Android`
in `DeviceRegistrationRequest` when exchanging the resulting authorization code.
The serial and PKCE verifier must come from the same sign-in URL. A new sign-in
is required; changing the profile does not convert existing credentials.

The Android profile uses a ten-byte serial, Android OAuth parameters and
registration metadata. Saved credentials retain the registered device type;
token refresh and API/download headers use that type after reload. The existing
`GetAuthURL` method and zero-value registration request keep the iPhone profile.

This is a Go implementation of the registration protocol described and tested
by the Libation/AudibleApi maintainers. It does not establish the meaning or
duration of every `CustomerThrottled` response.

References:

- [AudibleApi #84: Android serial correction and fresh-registration requirement](https://github.com/rmcrackan/AudibleApi/pull/84)
- [AudibleApi #83: registration profiles](https://github.com/rmcrackan/AudibleApi/pull/83)
- [Libation #2029: device registration](https://github.com/rmcrackan/Libation/pull/2029)
- [Maintainer recovery advice on Reddit](https://www.reddit.com/r/audible/comments/1wbbzdb/throttling/)

The Android serial correction applies to Android registrations. It does not
show that an iPhone serial should have the same length.
