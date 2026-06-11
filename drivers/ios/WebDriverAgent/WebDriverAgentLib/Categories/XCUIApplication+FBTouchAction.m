/**
 * Copyright (c) 2015-present, Facebook, Inc.
 * All rights reserved.
 *
 * This source code is licensed under the BSD-style license found in the
 * LICENSE file in the root directory of this source tree.
 */


#import "XCUIApplication+FBTouchAction.h"

#import "FBBaseActionsSynthesizer.h"
#import "FBConfiguration.h"
#import "FBExceptions.h"
#import "FBLogger.h"
#import "FBRunLoopSpinner.h"
#import "FBW3CActionsSynthesizer.h"
#import "FBXCTestDaemonsProxy.h"
#import "XCEventGenerator.h"
#import "XCUIElement+FBUtilities.h"

#if !TARGET_OS_TV

@implementation XCUIApplication (FBTouchAction)

+ (BOOL)handleEventSynthesWithError:(NSError *)error
{
  if ([error.localizedDescription containsString:@"not visible"]) {
    [[NSException exceptionWithName:FBElementNotVisibleException
                             reason:error.localizedDescription
                           userInfo:error.userInfo] raise];
  }
  return NO;
}

- (BOOL)fb_performActionsWithSynthesizerType:(Class)synthesizerType
                                     actions:(NSArray *)actions
                                elementCache:(FBElementCache *)elementCache
                                       error:(NSError **)error
{
  FBBaseActionsSynthesizer *synthesizer = [[synthesizerType alloc] initWithActions:actions
                                                                    forApplication:self
                                                                      elementCache:elementCache
                                                                             error:error];
  if (nil == synthesizer) {
    return NO;
  }
  XCSynthesizedEventRecord *eventRecord = [synthesizer synthesizeWithError:error];
  if (nil == eventRecord) {
    return [self.class handleEventSynthesWithError:*error];
  }
  return [self fb_synthesizeEvent:eventRecord error:error];
}

- (BOOL)fb_performW3CActions:(NSArray *)actions
                elementCache:(FBElementCache *)elementCache
                       error:(NSError **)error
{
  if (![self fb_performActionsWithSynthesizerType:FBW3CActionsSynthesizer.class
                                          actions:actions
                                     elementCache:elementCache
                                            error:error]) {
    return NO;
  }
  // PATCHED (maestro-runner fork): no post-action stabilization wait.
  // This wait does not protect correctness — it only delays returning to
  // the client after the gesture has already been delivered. Correctness
  // is protected where it matters: the PRE-action element lookup goes
  // through the page-source path, which still waits for UI stability
  // (FBXPath -> fb_waitUntilStableWithTimeout) before serializing, so
  // tap coordinates are never computed from mid-animation frames. The
  // post-action wait cost ~0.4-0.5s of pure idle per tap (~25-40 taps
  // per flow) because a React Native app under automation rarely
  // reports "stable" early.
  return YES;
}

- (BOOL)fb_synthesizeEvent:(XCSynthesizedEventRecord *)event error:(NSError *__autoreleasing*)error
{
  return [FBXCTestDaemonsProxy synthesizeEventWithRecord:event error:error];
}

@end
#endif
