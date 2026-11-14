export interface AuthStatus {
  authenticated: boolean;
  csrf_token?: string;
  tls: boolean;
}

export interface HubInfo {
  pid: number;
  httpsPort: number;
  httpPort: number;
  lsVersion: string;
}

export interface ConversationSummary {
  id: string;
  title: string;
  last_status: string;
  last_step_at: string;
  step_count: number;
  has_full_log: boolean;
}

export interface ToolCall {
  name: string;
  args?: unknown;
}

export interface TranscriptStep {
  step_index: number;
  source: string;
  type: string;
  status: string;
  created_at: string;
  content?: string;
  thinking?: string;
  tool_calls?: ToolCall[];
}

export interface TreeNode {
  name: string;
  path: string;
  is_dir: boolean;
  size?: number;
  language?: string;
  children?: TreeNode[];
}

export interface FileContent {
  path: string;
  name: string;
  language: string;
  size: number;
  content: string;
}

export interface DoctorCheck {
  name: string;
  status: 'PASS' | 'WARN' | 'FAIL';
  detail: string;
}

export interface SystemStatus {
  workspace: string;
  hubs: HubInfo[];
  firewall_rule_ok: boolean;
  firewall_detail: string;
  tls_enabled: boolean;
}
