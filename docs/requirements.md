# Requirements and their tests

Which tests cover each requirement of
[functional_requirements.md](functional_requirements.md), and which
requirements no test covers. Generated from the tests' doc comments by
`make requirements`; `go test ./tests/` fails when it is out of date.

A test names what it covers in its doc comment: a requirement (FR-006) or
one of its acceptance criteria (AC-006.2), or "(operations)" for what no
requirement covers, such as backups and the broker. Every integration and
end-to-end test names one; unit tests may.

## By requirement

| Requirement | Integration | End to end | Unit |
|:--|:--|:--|:--|
| FR-001 User Registration | TestChangedEmailNeedsConfirming, TestFailedSignupKeepsItsCode, TestSignupConfirmsPhoneAndEmail |  |  |
| FR-002 User Authentication | TestCodeBudget, TestDriverPINReset, TestDriversBehindOneAddress, TestExpiredCodesAreNotSent, TestLockoutStartsAgainAfterExpiry, TestPasswordReset, TestRateLimits, TestStaffResetByPhone, TestUnconfirmedStaffEmailGetsNoReset, TestWrongAppNeedsTheRightCredential | TestDriverAppInABrowser |  |
| FR-003 Session Management | TestDeactivatedDriverIsSignedOut, TestLiveFeedSessionCheck, TestPasswordReset |  |  |
| FR-004 Role-Based Access Control | TestAPIKeysAreTheAdminsAlone, TestLiveUpdatesByRole, TestStaffRoles, TestTeamPage |  |  |
| FR-005 Multi-Tenant Isolation | TestAnotherCompanysRowsAreRefused, TestCompaniesAreIsolated, TestReferencesCarryTheTenant, TestTwoInstances |  |  |
| FR-006 Shipment Creation | TestAmharicNamesFit, TestBookingEstimate, TestCargoPieces, TestCargoValueInSantim, TestConsignee, TestDeliveryLifecycle, TestPlannedPickup, TestShipmentCharges | TestDelivery |  |
| FR-007 Shipment Assignment | TestBulkAssign, TestDeliveryLifecycle, TestLoadLimitUnderALock, TestVehicleOnOneTripWithinCapacity | TestDelivery |  |
| FR-008 Shipment Status Updates | TestDeliveryLifecycle |  |  |
| FR-009 Shipment Search & Filtering | TestSavedViews, TestSearchByPhoneInAnyForm |  |  |
| FR-010 Shipment Details View | TestShipmentDocuments |  |  |
| FR-011 Shipment Editing | TestNewDestinationTextsTheDriver |  |  |
| FR-012 Shipment Cancellation | TestCancelReasonsFollowTheRoad, TestReopenFollowsTheRules |  |  |
| FR-013 Bulk Shipment Operations (in part not built: Appendix D) | TestBulkAssign, TestImportShipments |  |  |
| FR-014 Offline Data Storage |  | TestDriverApp, TestDriverAppInABrowser |  |
| FR-015 Connectivity Detection & Handling |  | TestDriverAppInABrowser |  |
| FR-016 Shipment List View (Driver) |  | TestDriverAppInABrowser |  |
| FR-017 Shipment Detail View (Driver) |  | TestDriverAppInABrowser |  |
| FR-018 Status Update (Driver) | TestDeliveredStraightFromAssigned, TestDeliveryLifecycle, TestRepeatDelayIsRecorded | TestDelivery, TestDriverAppInABrowser |  |
| FR-019 GPS Tracking | TestHeadingAwayFromTheDestination, TestPositionsKeptAfterReassignment, TestTrackingPartitions |  |  |
| FR-020 Proof of Delivery - Signature | TestDeliveredStraightFromAssigned, TestDeliveriesStoredInS3, TestDeliveryLifecycle, TestDeliveryMediaRecorded, TestHeldPODAfterReassignment, TestRecentRecipientsAreSuggested | TestDelivery |  |
| FR-021 Proof of Delivery - Photo | TestDeliveriesStoredInS3, TestDeliveryMediaRecorded, TestHugePhotoRefused, TestPresignedURLs, TestRefusedPODLeavesNoFiles | TestDelivery |  |
| FR-022 Delivery Location Verification | TestDeliveryAwayFromTheDestination |  |  |
| FR-023 Delivery Notes | TestDeliveryNotes |  |  |
| FR-024 SMS Fallback for Status Updates | TestDriverSMSCommands, TestInboundSMSOnceAndRepliesLimited, TestTextsThroughTheGateway |  |  |
| FR-025 Offline Map Support (in part not built: Appendix D) |  |  |  |
| FR-026 App Settings | TestDriverSettingsFollowTheDriver |  |  |
| FR-027 Dashboard Overview | TestLiveUpdatesByRole, TestTwoInstances |  |  |
| FR-028 Live Map View | TestFleetMapKeepsTheNewestPosition |  |  |
| FR-029 Shipment List View (Dispatcher) | TestSavedViews |  |  |
| FR-030 Shipment Detail View (Dispatcher) | TestALongTripEndsWhereTheTruckIs |  |  |
| FR-031 Driver Management | TestAmharicNamesFit, TestDeactivatedDriverIsSignedOut, TestDriverLicence, TestPhoneTakenElsewhereIsExplained |  |  |
| FR-032 Vehicle Management | TestVehicleOnOneTripWithinCapacity, TestVehiclePapers |  |  |
| FR-033 Notifications & Alerts | TestAlertBadgeIsPerPerson, TestAlertEmails, TestAlertRules, TestAlertsFromTheRoad, TestDeliveredAlertCanBeTurnedOff, TestDismissedAlertsStayDismissed, TestHeadingAwayFromTheDestination, TestLongStop, TestPlannedPickup, TestRepeatDelayIsRecorded, TestResolveAndRecipients |  |  |
| FR-034 Reports & Analytics | TestCancelReasonsFollowTheRoad, TestReportFilters |  |  |
| FR-035 Dispatcher Messaging | TestAlertsFromTheRoad, TestDriverMessagesAreTheOffice, TestFeedBroadcasts, TestManyMessagesArriveInPages, TestMessageEveryDriverOnTheRoad, TestOfficeMessageFallsBackToSMS |  |  |
| FR-036 Shipment Tracking (Customer) | TestCodeLimitsLookTheSame, TestCodeTriesCannotRace, TestCustomersInAmharic, TestDeliveryLifecycle, TestPublicTrackingIsMinimal, TestTrackingPageDetails | TestDelivery |  |
| FR-037 SMS Notifications (Customer) | TestConsignee, TestCustomersInAmharic, TestCustomersInEnglishByDefault, TestDeliveryReportBeforeTheTextIsSaved, TestGatewayFaults, TestSMSTexts, TestTextsThroughTheGateway | TestDelivery |  |
| FR-038 Delivery Confirmation (Customer) | TestDeliveryLifecycle | TestDelivery |  |
| FR-039 Automatic Sync Protocol | TestFeedBroadcasts, TestFeedDeliversLateCommits, TestFeedFullDownloads, TestFeedPageOfExactlyTheLimit, TestFeedRemovesWithReasons, TestManyMessagesArriveInPages, TestSlowPhoneClock, TestSyncReplayIsIdempotent |  |  |
| FR-040 Conflict Resolution | TestHeldPODAfterReassignment, TestHeldPODOnCancelledShipment, TestOfficeDecisionWinsOverStaleDriverUpdate, TestResentSyncIsNotAConflict, TestVersionsRaisedByEdits |  |  |
| FR-041 Data Retention | TestExportHoldsEverything, TestRemovePersonDetails, TestRetentionClearsAccountLinks, TestRetentionRemovesOldCustomerDetails |  |  |
| FR-042 Audit Logging | TestHistoryIsAppendOnly, TestPlatformAuditAndHealth |  |  |
| FR-043 Subscription Plans | TestCancelSubscription, TestCancelWithRenewalUnpaid, TestDowngradeWaitsForTheEndOfThePeriod, TestEnterpriseTerms, TestPayingForAPlan, TestTrialEndMakesTheAccountReadOnly, TestTrialNotices, TestUpgradeIsProrated |  |  |
| FR-044 Billing Management | TestCancelWithRenewalUnpaid, TestPayingByReference, TestPayingByReferenceThroughCBEBirr, TestPayingForAPlan, TestPayingThroughTelebirr, TestPaymentForAPaidInvoiceIsRefunded, TestRenewalAfterSuspension, TestRenewalKeepsTheBillingDay, TestRenewalOverdueAndBankTransfer, TestSignupWithAPaidPlan, TestTelebirrNotificationFaults, TestTextOverageIsInvoiced, TestUpgradeIsProrated | TestPayingWithCBEBirr, TestPayingWithTelebirr |  |
| FR-045 Usage Tracking | TestTextOverageIsInvoiced, TestTrialTextsStopAtTheAllowance, TestUsageAlerts |  |  |
| FR-046 Public API (in part not built: Appendix D) | TestAPIKeysAreTheAdminsAlone, TestIdempotencyKeys, TestPublicAPI, TestSpecificationMatchesTheAPI |  |  |
| FR-047 Webhooks (in part not built: Appendix D) | TestWebhooks, TestWebhooksStayOnTheInternet |  |  |
| FR-048 Company Onboarding | TestSignupConfirmsPhoneAndEmail, TestSignupWithAPaidPlan |  |  |
| FR-049 Platform Admin Dashboard | TestDeleteCompany, TestPlatformAdminStaysOnPlatform, TestPlatformAuditAndHealth, TestSupportTickets |  |  |

## Requirements no test names

- FR-025 Offline Map Support (in part not built: Appendix D)

## Tests of operations

| Test | Suite |
|:--|:--|
| TestBackupAndRestore | end to end |
| TestBrokerRetries | integration |
| TestEdge | end to end |
| TestGarageSetup | integration |
| TestMetrics | integration |
| TestMigrations | integration |
| TestMonitoring | end to end |
| TestPointInTimeRestore | end to end |
| TestProvidersWithOtherSettings | integration |
| TestRabbitMQ | integration |
| TestS3Storage | integration |
| TestScheduledJobs | integration |
| TestSeparateProcesses | integration |
| TestSharedRateLimits | integration |
| TestStoppingWhileSending | integration |
