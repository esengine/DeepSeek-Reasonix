// The account, which Reasonix needs only for the networked surfaces. Signing in
// is a device grant: the machine polls while the browser does the talking.
export interface AccountUser {
  handle: string;
  email: string;
  label: string;
}

// signedIn with an error means the token is still here but the identity service
// could not be reached — never the same thing as being signed out.
export interface AccountState {
  signedIn: boolean;
  user?: AccountUser;
  expired?: boolean;
  error?: string;
}

export interface DeviceGrant {
  deviceCode: string;
  userCode: string;
  verificationUri: string;
  verificationUriComplete: string;
  interval: number;
  expiresIn: number;
}
