import ReactReconciler from "react-reconciler";
import { type BoxProps, HOST_BOX, HOST_TEXT, type TextProps } from "../react/components.js";
import {
  type HostBox,
  type HostNode,
  type HostText,
  hostToLayoutNode,
  makeHostBox,
  makeHostRawText,
  makeHostText,
} from "./host-node.js";

export interface HostRoot {
  readonly children: HostNode[];
  onCommit: () => void;
}

type Container = HostRoot;
type Type = typeof HOST_BOX | typeof HOST_TEXT | string;
type Props = BoxProps | TextProps;
type Instance = HostBox | HostText;
type TextInstance = ReturnType<typeof makeHostRawText>;
type PublicInstance = Instance;
type HostContext = Record<string, never>;
type UpdatePayload = Props;
type ChildSet = never;
type TimeoutHandle = ReturnType<typeof setTimeout>;
type NoTimeout = -1;

const hostConfig: ReactReconciler.HostConfig<
  Type,
  Props,
  Container,
  Instance,
  TextInstance,
  never,
  never,
  PublicInstance,
  HostContext,
  UpdatePayload,
  ChildSet,
  TimeoutHandle,
  NoTimeout
> = {
  supportsMutation: true,
  supportsPersistence: false,
  supportsHydration: false,
  isPrimaryRenderer: false,
  noTimeout: -1,

  createInstance(type, props) {
    if (type === HOST_BOX) return makeHostBox(props as BoxProps);
    if (type === HOST_TEXT) return makeHostText(props as TextProps);
    throw new Error(`Reasonix renderer: unsupported element type: ${String(type)}`);
  },

  createTextInstance(text) {
    return makeHostRawText(text);
  },

  appendInitialChild(parent, child) {
    parent.children.push(child as HostNode);
    (child as HostNode).parent = parent;
  },

  appendChild(parent, child) {
    parent.children.push(child as HostNode);
    (child as HostNode).parent = parent;
  },

  appendChildToContainer(container, child) {
    container.children.push(child as HostNode);
    (child as HostNode).parent = null;
  },

  insertBefore(parent, child, beforeChild) {
    const idx = parent.children.indexOf(beforeChild as HostNode);
    if (idx < 0) parent.children.push(child as HostNode);
    else parent.children.splice(idx, 0, child as HostNode);
    (child as HostNode).parent = parent;
  },

  insertInContainerBefore(container, child, beforeChild) {
    const idx = container.children.indexOf(beforeChild as HostNode);
    if (idx < 0) container.children.push(child as HostNode);
    else container.children.splice(idx, 0, child as HostNode);
    (child as HostNode).parent = null;
  },

  removeChild(parent, child) {
    const idx = parent.children.indexOf(child as HostNode);
    if (idx >= 0) parent.children.splice(idx, 1);
    (child as HostNode).parent = null;
  },

  removeChildFromContainer(container, child) {
    const idx = container.children.indexOf(child as HostNode);
    if (idx >= 0) container.children.splice(idx, 1);
  },

  finalizeInitialChildren() {
    return false;
  },

  prepareUpdate(_instance, _type, _oldProps, newProps) {
    return newProps;
  },

  commitUpdate(instance, payload) {
    instance.props = payload;
  },

  commitTextUpdate(textInstance, _oldText, newText) {
    textInstance.text = newText;
  },

  resetTextContent() {
    /* no-op */
  },

  shouldSetTextContent() {
    return false;
  },

  getRootHostContext() {
    return {};
  },

  getChildHostContext() {
    return {};
  },

  getPublicInstance(instance) {
    return instance as Instance;
  },

  prepareForCommit() {
    return null;
  },

  resetAfterCommit(container) {
    container.onCommit();
  },

  preparePortalMount() {
    /* no-op */
  },

  scheduleTimeout: setTimeout,
  cancelTimeout: clearTimeout,

  getCurrentEventPriority() {
    return DefaultEventPriority;
  },

  getInstanceFromNode() {
    return null;
  },

  beforeActiveInstanceBlur() {
    /* no-op */
  },

  afterActiveInstanceBlur() {
    /* no-op */
  },

  prepareScopeUpdate() {
    /* no-op */
  },

  getInstanceFromScope() {
    return null;
  },

  detachDeletedInstance() {
    /* no-op */
  },

  clearContainer(container) {
    container.children.length = 0;
  },
};

const DefaultEventPriority = 16;

export const reconciler = ReactReconciler(hostConfig);

export { hostToLayoutNode };
