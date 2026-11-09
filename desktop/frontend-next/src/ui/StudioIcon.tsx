import type { SVGProps } from "react";
import {
  Activity, Archive, ArrowDown, ArrowRight, ArrowUp, Ban, BarChart3, BookOpen, Bookmark, Bot, Brain,
  Check, ChevronDown, ChevronRight, ChevronUp, Circle, CircleCheck, CircleDashed, CircleHelp,
  CirclePlay, CircleX, Clock, CodeXml, Coins, Copy, Download, Eclipse, Ellipsis, Eraser,
  ExternalLink, Eye, EyeOff, FileCode, FilePenLine, FileSearch, FileText, Folder, FolderInput,
  FolderOpen, Galaxy, GitBranch, GitFork, Globe, HardDrive, History, Image, Info, Keyboard,
  Layers, LayoutGrid, Link, List, ListChecks, LoaderCircle, Lock, Maximize2, Menu, MessageCircle,
  MessageSquare, MessageSquareText, Minimize2, Minus, Monitor, Moon, Network, NotebookPen, Package,
  Palette, PanelLeft, PanelLeftClose, PanelLeftOpen, PanelRight, Pause, Pencil, Pin, Play, Plug,
  Plus, QrCode, Quote, Regex, Repeat, RotateCcw, RotateCw, Search, Send, Server, Settings, Shield,
  ShieldCheck, SlidersHorizontal, Smartphone, Sparkle, Sparkles, Square, SquareCheck, Star, Sun,
  Target, Terminal, ThumbsDown, ThumbsUp, Trash2, TriangleAlert, Type, Upload, UserRound,
  Volume2, VolumeX, Wallet, Wrench, X, ZoomIn, ZoomOut, Zap,
} from "lucide-react";

const ICONS = {
  folder: Folder, reveal: FolderOpen, file: FileText, image: Image, send: Send, fail: CircleX,
  search: Search, chevron: ChevronRight, down: ChevronDown, up: ChevronUp, minus: Minus,
  brain: Brain, plus: Plus, arrow: ArrowUp, arrowDown: ArrowDown, arrowRight: ArrowRight,
  close: X, expand: Maximize2, collapseAll: Minimize2, expandAll: Maximize2, check: Check,
  globe: Globe, code: CodeXml, settings: Settings, sliders: SlidersHorizontal, more: Ellipsis,
  menu: Menu, panel: PanelLeft, panelClose: PanelLeftClose, panelOpen: PanelLeftOpen,
  panelRight: PanelRight, lock: Lock, shield: Shield, shieldCheck: ShieldCheck,
  branch: GitBranch, fork: GitFork, layers: Layers, spark: Sparkle, agent: Bot,
  refresh: RotateCw, sparkle: Star, target: Target, terminal: Terminal, eraser: Eraser,
  clock: Clock, stop: Square, play: Play, pause: Pause, bolt: Zap, coin: Coins,
  info: Info, warning: TriangleAlert, copy: Copy, quote: Quote, external: ExternalLink,
  plug: Plug, download: Download, upload: Upload, chat: MessageSquare, list: List,
  edit: Pencil, rewind: RotateCcw, pin: Pin, trash: Trash2, archive: Archive, server: Server,
  link: Link, feedback: MessageSquareText, wallet: Wallet, eye: Eye, eyeoff: EyeOff,
  activity: Activity, chart: BarChart3, book: BookOpen, bookmark: Bookmark, question: CircleHelp,
  circle: Circle, circleDashed: CircleDashed, fading: Eclipse, complete: SquareCheck,
  success: CircleCheck, running: CirclePlay, inactive: Circle, loading: LoaderCircle,
  fileCode: FileCode, fileEdit: FilePenLine, fileSearch: FileSearch, moveFile: FolderInput,
  history: History, storage: HardDrive, keyboard: Keyboard, grid: LayoutGrid, monitor: Monitor,
  sun: Sun, moon: Moon, network: Network, notebook: NotebookPen, package: Package,
  palette: Palette, repeat: Repeat, galaxy: Galaxy, phone: Smartphone, qr: QrCode, user: UserRound,
  tasks: ListChecks, tools: Wrench, magic: Sparkles, comment: MessageCircle, matchCase: Type,
  regex: Regex, thumbsUp: ThumbsUp, thumbsDown: ThumbsDown, volume: Volume2, muted: VolumeX,
  zoomIn: ZoomIn, zoomOut: ZoomOut, disabled: Ban,
} as const;

export type StudioIconName = keyof typeof ICONS;

export function StudioIcon({ name, className, ...props }: SVGProps<SVGSVGElement> & { name: StudioIconName }) {
  const Icon = ICONS[name];
  return <Icon className={["studio-icon", className].filter(Boolean).join(" ")} size={16} strokeWidth={1.8} aria-hidden="true" focusable="false" {...props} />;
}
