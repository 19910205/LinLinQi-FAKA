export interface Category {
  id: string;
  name: string;
  slug: string;
  description: string;
  icon: string;
  image_url?: string;
}

export interface CatalogMedia {
  id: string;
  role: "cover" | "gallery" | "detail";
  sort: number;
  alt_text: string;
  url: string;
  mime: string;
}

export interface Product {
  id: string;
  category_id: string;
  name: string;
  slug: string;
  summary: string;
  description: string;
  cover_url?: string;
  media?: CatalogMedia[];
  price: number;
  compare_price: number;
  currency: string;
  source_currency?: string;
  fx?: CurrencyQuote;
  sold_count: number;
  delivery_type: string;
  minimum: number;
  maximum: number;
  featured: boolean;
  tags: string;
  category: Category;
}

export interface ProductItem {
  product: Product;
  stock: number;
  variants?: ProductVariant[];
  input_fields?: ProductInputField[];
}

export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface ProductQuery {
  q?: string;
  category?: string;
  page?: number;
  page_size?: number;
  featured?: boolean;
  currency?: string;
}

export interface CurrencyQuote {
  snapshot_id?: string;
  source_currency: string;
  target_currency: string;
  rate: string;
  source_tier: "live" | "manual" | "cached" | "system" | string;
  expires_at?: string;
}

export type ProductPage = PageResult<ProductItem>;

export type PublicBannerPlacement =
  "home_hero" | "home_secondary" | "content_header";

export interface PublicBanner {
  id: string;
  title: string;
  image_url: string;
  target_url: string;
  placement: PublicBannerPlacement;
}

export interface PublicPost {
  id: string;
  title: string;
  slug: string;
  summary: string;
  content: string;
  cover_url: string;
  published_at?: string | null;
}

export interface PublicAnnouncement {
  id: string;
  title: string;
  content: string;
  level: string;
  created_at: string;
}

export interface PublicContent {
  banners: PublicBanner[];
  posts: PublicPost[];
  announcements: PublicAnnouncement[];
}

export interface ProductInputField {
  id: string;
  key: string;
  label: string;
  input_type: "text" | "email" | "number" | "select" | "textarea";
  required: boolean;
  sensitive: boolean;
  placeholder: string;
  help_text: string;
  options: string[];
  validation_pattern?: string;
  min_length: number;
  max_length: number;
  sort: number;
}

export interface CheckoutInputValue {
  product_id?: string;
  variant_id?: string;
  field_id: string;
  value: string;
}

export interface ProductVariant {
  id: string;
  sku: string;
  name: string;
  attributes: string;
  price: number;
  compare_price: number;
  sort: number;
  purchase_limit: number;
  stock: number;
}

export interface PriceAdjustment {
  code: string;
  label: string;
  amount: number;
}

export interface PriceQuote {
  unit_price: number;
  quantity: number;
  subtotal: number;
  discount: number;
  total: number;
  adjustments: PriceAdjustment[];
  currency?: string;
}

export interface CartItem {
  id: string;
  product_id: string;
  variant_id?: string | null;
  quantity: number;
  product?: Product;
  variant?: ProductVariant;
  stock?: number;
  available: boolean;
  quote?: PriceQuote;
  input_fields?: ProductInputField[];
}

export interface Cart {
  id?: string;
  guest_token: string;
  currency: string;
  expires_at?: string;
  items: CartItem[];
  fx?: CurrencyQuote;
}

export interface CheckoutQuoteLine {
  product_id: string;
  variant_id?: string;
  product_name: string;
  variant_name?: string;
  quote: PriceQuote;
  stock: number;
  available: boolean;
}

export interface CheckoutQuote {
  lines: CheckoutQuoteLine[];
  subtotal: number;
  discount: number;
  coupon_discount: number;
  fee: number;
  total: number;
  adjustments: PriceAdjustment[];
  currency?: string;
  fx: CurrencyQuote;
}

export interface OrderItem {
  id: string;
  product_name: string;
  unit_price: number;
  currency: string;
  quantity: number;
  card_content: string;
  variant_id?: string;
  variant_name?: string;
}

export interface Order {
  id: string;
  order_no: string;
  lookup_token?: string;
  email: string;
  status: string;
  payment_status: string;
  payment_method?: string;
  currency: string;
  subtotal: number;
  discount: number;
  adjustments: PriceAdjustment[];
  total: number;
  created_at: string;
  items: OrderItem[];
}

export interface OrderLookupCredential {
  order_no: string;
  lookup_token: string;
  saved_at: string;
}

export interface WebhookEndpoint {
  id: string;
  url: string;
  events: string | string[];
  enabled: boolean;
  failure_count: number;
  disabled_at?: string | null;
  created_at: string;
}

export type APICredentialStatus =
  "pending" | "active" | "suspended" | "revoked";

export interface APICredential {
  id: string;
  name: string;
  key: string;
  permissions: string;
  status: APICredentialStatus;
  last_used_at?: string | null;
  revoked_at?: string | null;
  created_at: string;
}

export interface APICredentialCreateResult {
  credential: APICredential;
  secret: string;
  notice?: string;
}

export interface WebhookCreateResult {
  secret: string;
  webhook?: WebhookEndpoint;
  endpoint?: WebhookEndpoint;
}

export type TicketCategory =
  "billing" | "delivery" | "product" | "refund" | "other";

export type TicketStatus =
  "open" | "in_progress" | "waiting_user" | "resolved" | "closed";

export interface SupportTicket {
  id: string;
  ticket_no: string;
  order_id?: string | null;
  category: TicketCategory;
  subject: string;
  priority: string;
  status: TicketStatus;
  last_message_at?: string | null;
  user_unread: number;
  created_at: string;
  updated_at: string;
  closed_at?: string | null;
}

export interface TicketMessage {
  id: string;
  ticket_id: string;
  author_type: "user" | "admin";
  body: string;
  created_at: string;
}

export interface TicketDetail {
  ticket: SupportTicket;
  messages: TicketMessage[];
}

export interface TicketPage {
  items: SupportTicket[];
  total: number;
  page: number;
  page_size: number;
}

export interface CreateTicketPayload {
  category: TicketCategory;
  subject: string;
  body: string;
  order_id?: string;
}

export interface GiftCardRecord {
  id: string;
  code_preview: string;
  initial_balance: number;
  balance: number;
  currency: string;
  status: "active" | "redeemed" | "expired" | "disabled" | string;
  redeemed_at?: string | null;
  expires_at?: string | null;
  created_at: string;
}

export interface GiftCardEntry {
  id: string;
  gift_card_id: string;
  amount: number;
  balance_after: number;
  type: string;
  created_at: string;
}

export interface GiftCardRedeemResult {
  card: GiftCardRecord;
  entry: GiftCardEntry;
  wallet_balance: number;
}

export interface WalletAccount {
  id: string;
  currency: string;
  balance: number;
  frozen: number;
  available: number;
  minor_unit: number;
  symbol: string;
  currency_enabled: boolean;
}

export interface WalletEntry {
  id: string;
  entry_no: string;
  type: string;
  amount: number;
  balance_after: number;
  reference_type?: string;
  description: string;
  created_at: string;
  currency: string;
}

export interface WalletData {
  account: WalletAccount;
  accounts: WalletAccount[];
  selected_currency: string;
  entries: WalletEntry[];
}

export interface RechargeOrder {
  id: string;
  recharge_no: string;
  amount: number;
  bonus: number;
  currency: string;
  credit_amount: number;
  credit_currency: string;
  fx_snapshot_id?: string | null;
  channel_code: string;
  channel_name: string;
  status:
    | "creating"
    | "pending"
    | "succeeded"
    | "failed"
    | "expired"
    | "cancelled"
    | string;
  checkout_url?: string;
  expires_at: string;
  paid_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface OAuthProvider {
  code: string;
  name: string;
}

export type AffiliateStatus = "pending" | "active" | "suspended" | "rejected";

export interface AffiliateProfile {
  id: string;
  referral_code: string;
  commission_basis_point: number;
  status: AffiliateStatus;
  total_commission: number;
  available_commission: number;
  frozen_commission: number;
  currency: string;
  applied_at: string;
  approved_at?: string | null;
  rejected_at?: string | null;
}

export interface AffiliateCommission {
  id: string;
  order_id: string;
  order_amount: number;
  commission: number;
  reversed_amount: number;
  currency: string;
  status: "pending" | "available" | "partially_reversed" | "reversed" | string;
  settles_at: string;
  settled_at?: string | null;
  created_at: string;
}

export interface AffiliateWithdrawal {
  id: string;
  withdrawal_no: string;
  amount: number;
  fee: number;
  currency: string;
  method: "alipay" | "bank" | "usdt" | string;
  account_preview: string;
  status: "pending" | "processing" | "completed" | "rejected" | string;
  payout_reference?: string;
  reason?: string;
  processed_at?: string | null;
  created_at: string;
}

export interface AffiliateData {
  profile: AffiliateProfile | null;
  commissions: AffiliateCommission[];
  withdrawals: AffiliateWithdrawal[];
  balances: Array<{
    currency: string;
    total_commission: number;
    available_commission: number;
    frozen_commission: number;
  }>;
  referral_count: number;
  referral_link?: string;
}

export type ResellerStatus = "pending" | "active" | "suspended" | "rejected";

export interface ResellerProfile {
  id: string;
  user_id: string;
  name: string;
  code: string;
  status: ResellerStatus;
  credit_limit: number;
  wholesale_level: number;
  applied_at: string;
  verified_at?: string | null;
  rejected_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ResellerDomain {
  id: string;
  reseller_id: string;
  domain: string;
  status:
    | "pending_verification"
    | "verified"
    | "active"
    | "suspended"
    | "rejected"
    | string;
  tls_status:
    "pending" | "provisioning" | "active" | "failed" | "disabled" | string;
  verification_token: string;
  verified_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ResellerTheme {
  mode: "light" | "dark" | "system";
  density: "comfortable" | "compact";
}

export interface ResellerSEO {
  title: string;
  description: string;
}

export interface ResellerSupport {
  email: string;
  url: string;
}

export interface ResellerSite {
  id: string;
  reseller_id: string;
  site_name: string;
  logo_url: string;
  theme: string | Partial<ResellerTheme>;
  seo: string | Partial<ResellerSEO>;
  support: string | Partial<ResellerSupport>;
  created_at: string;
  updated_at: string;
}

export interface ResellerProductRule {
  id: string;
  reseller_id: string;
  product_id: string;
  variant_id?: string | null;
  enabled: boolean;
  pricing_mode: "markup" | "fixed";
  markup_basis_point: number;
  fixed_price: number;
  created_at: string;
  updated_at: string;
}

export interface ResellerWallet {
  id: string;
  owner_type: string;
  owner_id: string;
  currency: string;
  balance: number;
  frozen: number;
  version: number;
  created_at: string;
  updated_at: string;
}

export interface ResellerCreditState {
  balance: number;
  frozen: number;
  limit: number;
  exposure: number;
  remaining: number;
  breached: boolean;
}

export interface ResellerWholesalePolicy {
  level: number;
  name: string;
  discount_basis_point: number;
  configured: boolean;
  enabled: boolean;
}

export interface ResellerOverview {
  profile: ResellerProfile;
  domains: ResellerDomain[];
  site: ResellerSite;
  product_rules: ResellerProductRule[];
  wallet: ResellerWallet;
  credit: ResellerCreditState;
  wholesale: ResellerWholesalePolicy;
}

export interface ResellerCatalogItem {
  product: Product;
  stock: number;
  variants: ProductVariant[];
  rules: ResellerProductRule[];
}

export interface ResellerOrder {
  id: string;
  order_no: string;
  status: string;
  payment_status: string;
  total: number;
  currency: string;
  margin: number;
  created_at: string;
}

export interface ResellerWithdrawal {
  id: string;
  withdrawal_no: string;
  amount: number;
  fee: number;
  method: "alipay" | "bank" | "usdt" | string;
  account_preview: string;
  status: "pending" | "processing" | "completed" | "rejected" | string;
  payout_reference?: string;
  reason?: string;
  processed_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ResellerPage<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
}

export interface ResellerSitePayload {
  site_name: string;
  logo_url: string;
  theme: ResellerTheme;
  seo: ResellerSEO;
  support: ResellerSupport;
}

export interface ResellerProductRulePayload {
  variant_id?: string;
  enabled: boolean;
  pricing_mode: "markup" | "fixed";
  markup_basis_point: number;
  fixed_price: number;
}

export interface ResellerDomainCreateResult {
  domain: ResellerDomain;
  dns_name: string;
  dns_value: string;
}
