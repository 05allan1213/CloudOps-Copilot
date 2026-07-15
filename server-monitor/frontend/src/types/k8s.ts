import type {
  K8sNodeSummary,
  K8sPodSummary,
  K8sDeploymentSummary,
  K8sServiceSummary,
  K8sIngressSummary,
  K8sEventSummary,
  K8sPVSummary,
  K8sPVCSummary,
  K8sResourceQuotaSummary,
  K8sLimitRangeSummary,
  K8sConfigMapSummary,
  K8sHPASummary,
  K8sDaemonSetSummary,
  K8sStatefulSetSummary,
  K8sJobSummary,
  K8sTopologyData,
} from "./index";

/** Node readiness statistics */
export interface K8sNodeStats {
  total: number;
  ready: number;
  not_ready: number;
}

/** Pod phase statistics */
export interface K8sPodStats {
  total: number;
  running: number;
  pending: number;
  failed: number;
  succeeded: number;
}

/** Deployment availability statistics */
export interface K8sDeploymentStats {
  total: number;
  available: number;
  unavailable: number;
}

/** Host coverage statistics for K8s nodes */
export interface K8sHostCoverageStats {
  total_nodes: number;
  covered_nodes: number;
  uncovered_nodes: number;
}

/** Cluster overview returned by the K8s dashboard API */
export interface K8sClusterOverview {
  nodes: K8sNodeStats;
  nodes_available: boolean;
  pods: K8sPodStats;
  deployments: K8sDeploymentStats;
  recent_events: K8sEventSummary[];
  host_coverage: K8sHostCoverageStats;
  truncated: boolean;
  collected_at: string;
}

/** A single node condition entry */
export interface K8sNodeCondition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
}

/** Extended node summary with conditions (not present in K8sNodeSummary from index.ts) */
export interface K8sNodeSummaryWithConditions extends K8sNodeSummary {
  conditions?: K8sNodeCondition[];
}

/** Host association info for a K8s node */
export interface K8sHostAssociation {
  online: boolean;
  last_scrape?: string;
}

/** Node with host monitoring association */
export interface K8sNodeWithHost {
  node: K8sNodeSummaryWithConditions;
  host_online: boolean;
  last_scrape?: string;
}

/** Detailed node view including pods and events */
export interface K8sNodeDetail {
  node: K8sNodeSummaryWithConditions;
  host_online: boolean;
  last_scrape?: string;
  pods: K8sPodSummary[];
  events: K8sEventSummary[];
  collection_errors?: string[];
}

/** Paginated node list result */
export interface K8sNodeListResult {
  items: K8sNodeWithHost[];
  total: number;
}

/** Paginated pod list result */
export interface K8sPodListResult {
  items: K8sPodSummary[];
  total: number;
}

/** Paginated deployment list result */
export interface K8sDeploymentListResult {
  items: K8sDeploymentSummary[];
  total: number;
}

/** Paginated service list result */
export interface K8sServiceListResult {
  items: K8sServiceSummary[];
  total: number;
}

/** Paginated ingress list result */
export interface K8sIngressListResult {
  items: K8sIngressSummary[];
  total: number;
}

/** Query parameters for listing Ingresses */
export interface K8sIngressQuery {
  namespace?: string;
  search?: string;
  limit?: number;
}

/** Paginated event list result */
export interface K8sEventListResult {
  items: K8sEventSummary[];
  total: number;
}

/** Paginated configmap list result */
export interface K8sConfigMapListResult {
  items: K8sConfigMapSummary[];
  total: number;
}

/** Query parameters for listing nodes */
export interface K8sNodeQuery {
  status?: string;
  role?: string;
  search?: string;
  limit?: number;
}

/** Query parameters for listing pods */
export interface K8sPodQuery {
  cluster?: string;
  namespace?: string;
  phase?: string;
  search?: string;
  limit?: number;
}

/** Query parameters for listing deployments */
export interface K8sDeploymentQuery {
  cluster?: string;
  namespace?: string;
  search?: string;
  limit?: number;
}

/** Query parameters for listing services */
export interface K8sServiceQuery {
  cluster?: string;
  namespace?: string;
  type?: string;
  search?: string;
  limit?: number;
}

/** Query parameters for listing events */
export interface K8sEventQuery {
  cluster?: string;
  namespace?: string;
  type?: string;
  search?: string;
  limit?: number;
}

/** Query parameters for listing configmaps */
export interface K8sConfigMapQuery {
  namespace?: string;
  search?: string;
  limit?: number;
}

/** Paginated PV list result */
export interface K8sPVListResult {
  items: K8sPVSummary[];
  total: number;
}

/** Paginated PVC list result */
export interface K8sPVCListResult {
  items: K8sPVCSummary[];
  total: number;
}

/** Paginated ResourceQuota list result */
export interface K8sResourceQuotaListResult {
  items: K8sResourceQuotaSummary[];
  total: number;
}

/** Paginated LimitRange list result */
export interface K8sLimitRangeListResult {
  items: K8sLimitRangeSummary[];
  total: number;
}

/** Query parameters for listing PVs */
export interface K8sPVQuery {
  status?: string;
  search?: string;
  limit?: number;
}

/** Query parameters for listing PVCs */
export interface K8sPVCQuery {
  namespace?: string;
  status?: string;
  search?: string;
  limit?: number;
}

/** Query parameters for listing ResourceQuotas */
export interface K8sResourceQuotaQuery {
  namespace?: string;
  search?: string;
  limit?: number;
}

/** Query parameters for listing LimitRanges */
export interface K8sLimitRangeQuery {
  namespace?: string;
  search?: string;
  limit?: number;
}

/** Paginated HPA list result */
export interface K8sHPAListResult {
  items: K8sHPASummary[];
  total: number;
}

/** Query parameters for listing HPAs */
export interface K8sHPAQuery {
  namespace?: string;
  search?: string;
  limit?: number;
}

/** Paginated DaemonSet list result */
export interface K8sDaemonSetListResult {
  items: K8sDaemonSetSummary[];
  total: number;
}

/** Query parameters for listing DaemonSets */
export interface K8sDaemonSetQuery {
  namespace?: string;
  search?: string;
  limit?: number;
}

/** Paginated StatefulSet list result */
export interface K8sStatefulSetListResult {
  items: K8sStatefulSetSummary[];
  total: number;
}

/** Query parameters for listing StatefulSets */
export interface K8sStatefulSetQuery {
  namespace?: string;
  search?: string;
  limit?: number;
}

/** Paginated Job list result */
export interface K8sJobListResult {
  items: K8sJobSummary[];
  total: number;
}

/** Query parameters for listing Jobs */
export interface K8sJobQuery {
  namespace?: string;
  status?: string;
  search?: string;
  limit?: number;
}

/** Re-export topology types */
export type { K8sTopologyData };
