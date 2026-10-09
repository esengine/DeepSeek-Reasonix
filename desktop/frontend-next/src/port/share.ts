// The window's door for phones: a second listener on a private address that a
// paired device reaches the same panes through. Mirrors serve.ShareStatus,
// serve.ShareOffer and serve.DeviceView.

// lan is Wi-Fi or Ethernet; virtual is a VPN or proxy tunnel a phone usually
// cannot reach. The kernel lists them in that order, so the first is the default.
export type AddressKind = "lan" | "tailnet" | "virtual";

export interface ShareAddress {
  interface: string;
  ip: string;
  kind: AddressKind;
}

export interface PairedDevice {
  id: string;
  name: string;
  pairedAt: string;
  lastSeen: string;
  // Holding an event stream open now: a page on screen.
  online: boolean;
}

export interface CloudDevice {
  id: string;
  name: string;
  connectedAt: string;
  lastSeen: string;
  ordinal: number;
}

export interface CloudRemoteStatus {
  deviceId?: string;
  name?: string;
  online: boolean;
  error?: string;
  reason?: "signed_out" | "relay_unreachable" | "relay_refused";
}

// A paired device's answer about itself. Mirrors serve.DeviceSelf.
export interface DeviceSelf {
  id: string;
  // The number the window lists it under, so both say 设备 N.
  ordinal: number;
  machine: string;
}

export interface ShareStatus {
  open: boolean;
  // The fixed port the user chose; absent means the system picks one each time.
  port?: number;
  origin?: string;
  addresses: ShareAddress[];
  devices: PairedDevice[];
  cloudDevices: CloudDevice[];
  cloudRemote?: CloudRemoteStatus;
  offerExpires?: string;
}

export interface ShareOffer {
  url: string;
  // An SVG document, drawn dark on light whatever the theme so it scans.
  qr: string;
  expires: string;
}

export interface CloudShareOffer {
  url: string;
  qr: string;
}

export interface SharePort {
  // Null where this kernel has no window to share from: a browser tab, or a
  // phone that is itself paired.
  shareStatus(): Promise<ShareStatus | null>;
  openShare(ip: string): Promise<ShareStatus>;
  closeShare(): Promise<ShareStatus>;
  // Zero clears the choice. Used the next time the door opens.
  setSharePort(port: number): Promise<ShareStatus>;
  offerShare(): Promise<ShareOffer>;
  offerCloudShare(): Promise<CloudShareOffer>;
  revokeDevice(id: string): Promise<ShareStatus>;
  // What this page is to the kernel: a paired device, or null for the window
  // and for a browser on a networked serve.
  device(): Promise<DeviceSelf | null>;
  // Unpairs this device from its own side.
  leaveDevice(): Promise<void>;
}
