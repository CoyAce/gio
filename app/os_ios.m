// SPDX-License-Identifier: Unlicense OR MIT

// +build darwin,ios

@import UIKit;

#include <stdint.h>
#include "_cgo_export.h"
#include "framework_ios.h"

__attribute__ ((visibility ("hidden"))) Class gio_layerClass(void);

@interface GioView: UIView <UIKeyInput,UITextViewDelegate>
@property uintptr_t handle;
@property (nonatomic, strong) UITextView *textView;
@end

@implementation GioViewController

CGFloat _keyboardHeight;

- (void)loadView {
	gio_runMain();

	CGRect zeroFrame = CGRectMake(0, 0, 0, 0);
	self.view = [[UIView alloc] initWithFrame:zeroFrame];
	self.view.layoutMargins = UIEdgeInsetsMake(0, 0, 0, 0);
	UIView *drawView = [[GioView alloc] initWithFrame:zeroFrame];
	[self.view addSubview: drawView];
#if !TARGET_OS_TV
	drawView.multipleTouchEnabled = YES;
#endif
	drawView.preservesSuperviewLayoutMargins = YES;
	drawView.layoutMargins = UIEdgeInsetsMake(0, 0, 0, 0);
	onCreate((__bridge CFTypeRef)drawView, (__bridge CFTypeRef)self);
#if !TARGET_OS_TV
	[[NSNotificationCenter defaultCenter] addObserver:self
											 selector:@selector(keyboardWillChange:)
												 name:UIKeyboardWillShowNotification
											   object:nil];
	[[NSNotificationCenter defaultCenter] addObserver:self
											 selector:@selector(keyboardWillChange:)
												 name:UIKeyboardWillChangeFrameNotification
											   object:nil];
	[[NSNotificationCenter defaultCenter] addObserver:self
											 selector:@selector(keyboardWillHide:)
												 name:UIKeyboardWillHideNotification
											   object:nil];
#endif
	[[NSNotificationCenter defaultCenter] addObserver: self
											 selector: @selector(applicationDidEnterBackground:)
												 name: UIApplicationDidEnterBackgroundNotification
											   object: nil];
	[[NSNotificationCenter defaultCenter] addObserver: self
											 selector: @selector(applicationWillEnterForeground:)
												 name: UIApplicationWillEnterForegroundNotification
											   object: nil];
}

- (void)applicationWillEnterForeground:(UIApplication *)application {
	GioView *view = (GioView *)self.view.subviews[0];
	if (view != nil) {
		onStart(view.handle);
	}
}

- (void)applicationDidEnterBackground:(UIApplication *)application {
	GioView *view = (GioView *)self.view.subviews[0];
	if (view != nil) {
		onStop(view.handle);
	}
}

- (void)viewDidDisappear:(BOOL)animated {
	[super viewDidDisappear:animated];
	GioView *view = (GioView *)self.view.subviews[0];
	onDestroy(view.handle);
}

- (void)viewDidLayoutSubviews {
	[super viewDidLayoutSubviews];
	GioView *view = (GioView *)self.view.subviews[0];
	CGRect frame = self.view.bounds;
	// Adjust view bounds to make room for the keyboard.
	frame.size.height -= _keyboardHeight;
	view.frame = frame;
	gio_onDraw(view.handle);
}

- (void)didReceiveMemoryWarning {
	onLowMemory();
	[super didReceiveMemoryWarning];
}

#if !TARGET_OS_TV
- (void)keyboardWillChange:(NSNotification *)note {
	NSDictionary *userInfo = note.userInfo;
	CGRect f = [userInfo[UIKeyboardFrameEndUserInfoKey] CGRectValue];
	_keyboardHeight = f.size.height;
	[self.view setNeedsLayout];
}

- (void)keyboardWillHide:(NSNotification *)note {
	_keyboardHeight = 0.0;
	[self.view setNeedsLayout];
}
#endif
@end

static void handleTouches(int last, GioView *view, NSSet<UITouch *> *touches, UIEvent *event) {
	CGFloat scale = view.contentScaleFactor;
	NSUInteger i = 0;
	NSUInteger n = [touches count];
	for (UITouch *touch in touches) {
		CFTypeRef touchRef = (__bridge CFTypeRef)touch;
		i++;
		NSArray<UITouch *> *coalescedTouches = [event coalescedTouchesForTouch:touch];
		NSUInteger j = 0;
		NSUInteger m = [coalescedTouches count];
		for (UITouch *coalescedTouch in [event coalescedTouchesForTouch:touch]) {
			CGPoint loc = [coalescedTouch locationInView:view];
			j++;
			int lastTouch = last && i == n && j == m;
			onTouch(view.handle, lastTouch, touchRef, touch.phase, loc.x*scale, loc.y*scale, [coalescedTouch timestamp]);
		}
	}
}

@implementation GioView {
    NSArray<UIKeyCommand *> *_keyCommands;
    NSString *_lastConfirmedText;
}
+ (void)onFrameCallback:(CADisplayLink *)link {
       gio_onFrameCallback((__bridge CFTypeRef)link);
}
+ (Class)layerClass {
    return gio_layerClass();
}

- (BOOL)becomeFirstResponder {
    return [self.textView becomeFirstResponder];
}

- (BOOL)resignFirstResponder {
    return [self.textView resignFirstResponder];
}

#pragma mark - UITextViewDelegate

/**
 * Listen for text changes - Handle all insertion operations
 *
 * Note: This method is called only AFTER the text has "actually changed",
 * the markedTextRange state is completely accurate at this point
 * Pinyin input (markedTextRange != nil): return directly, no insertion handling
 */
- (void)textViewDidChange:(UITextView *)textView {
    // Pinyin composing in progress: text not confirmed
    if (textView.markedTextRange != nil) {
        return;
    }

    NSString *currentText = textView.text;
    NSString *oldText = _lastConfirmedText ?: @"";

    if (currentText.length > oldText.length) {
        NSString *inserted = [currentText substringFromIndex:oldText.length];
        onText(self.handle, (__bridge CFTypeRef)inserted);
    }

    // Update cache, prepare for next change
    _lastConfirmedText = currentText;
}

/**
 * Intercept upcoming text changes - Handle all deletion operations and the deletion part of replacement operations.
 *
 * Note: The markedTextRange state is unreliable when this method is called, cannot be used to determine Pinyin input.
 * Deletion operation: Report immediately, let the system perform the actual deletion.
 * Replacement operation: Report deletion first, then report insertion (insertion is handled in textViewDidChange)
 */
- (BOOL)textView:(UITextView *)textView shouldChangeTextInRange:(NSRange)range replacementText:(NSString *)text {
    // Pinyin input in progress
    if (textView.markedTextRange != nil) {
        return YES;
    }

    // ------------------- Scenario 1: Pure deletion operation -------------------
    // Characteristic: replacementText is empty string
    // Trigger: Press backspace key, select text and press backspace key
    if (text.length == 0) {
        onDeleteBackward(self.handle);
        return YES;  // Let the system perform the actual deletion
    }

    // ------------------- Scenario 2: Replacement operation -------------------
    // Characteristic: Has deletion (range.length > 0) and has insertion (text.length > 0)
    // Trigger: Select text and directly input new characters, Chinese input method candidate word replacement
    if (text.length > 0 && range.length > 0) {
        // First report the deleted characters
        for (NSUInteger i = 0; i < range.length; i++) {
            onDeleteBackward(self.handle);
        }
        // Update cache: Simulate text state after deletion
        _lastConfirmedText = [textView.text stringByReplacingCharactersInRange:range withString:@""];
        // Note: Insertion operation is handled uniformly in textViewDidChange
        // onText is not called here to avoid duplicate reporting
        return YES;  // Let the system perform deletion + insertion
    }

    // ------------------- Scenario 3: Pure insertion operation -------------------
    // Characteristic: No deletion (range.length == 0) and has insertion (text.length > 0)
    // Trigger: Normal character input, paste
    // Insertion operations are handled uniformly in textViewDidChange, only return YES here to let the system execute
    return YES;
}

- (void)textViewDidEndEditing:(UITextView *)textView {
    self.textView.text = @"";
    _lastConfirmedText = @"";
}

- (instancetype)initWithFrame:(CGRect)frame {
    self = [super initWithFrame:frame];
    if (self) {
        _textView = [[UITextView alloc] initWithFrame:CGRectMake(0, 0, 0, 0)];
        _textView.delegate = self;
        _textView.selectable = NO;
        _textView.editable = YES;
        _textView.userInteractionEnabled = NO;
        _textView.hidden = YES;
        [self addSubview:_textView];
    }
    return self;
}

- (void)willMoveToWindow:(UIWindow *)newWindow {
	if (self.window != nil) {
		[[NSNotificationCenter defaultCenter] removeObserver:self
														name:UIWindowDidBecomeKeyNotification
													  object:self.window];
		[[NSNotificationCenter defaultCenter] removeObserver:self
														name:UIWindowDidResignKeyNotification
													  object:self.window];
	}
	self.contentScaleFactor = newWindow.screen.nativeScale;
	[[NSNotificationCenter defaultCenter] addObserver:self
											 selector:@selector(onWindowDidBecomeKey:)
												 name:UIWindowDidBecomeKeyNotification
											   object:newWindow];
	[[NSNotificationCenter defaultCenter] addObserver:self
											 selector:@selector(onWindowDidResignKey:)
												 name:UIWindowDidResignKeyNotification
											   object:newWindow];
}

- (void)onWindowDidBecomeKey:(NSNotification *)note {
	if (self.isFirstResponder) {
		onFocus(self.handle, YES);
	}
}

- (void)onWindowDidResignKey:(NSNotification *)note {
	if (self.isFirstResponder) {
		onFocus(self.handle, NO);
	}
}

- (void)touchesBegan:(NSSet<UITouch *> *)touches withEvent:(UIEvent *)event {
	handleTouches(0, self, touches, event);
}

- (void)touchesMoved:(NSSet<UITouch *> *)touches withEvent:(UIEvent *)event {
	handleTouches(0, self, touches, event);
}

- (void)touchesEnded:(NSSet<UITouch *> *)touches withEvent:(UIEvent *)event {
	handleTouches(1, self, touches, event);
}

- (void)touchesCancelled:(NSSet<UITouch *> *)touches withEvent:(UIEvent *)event {
	handleTouches(1, self, touches, event);
}

- (void)insertText:(NSString *)text {
}

- (BOOL)canBecomeFirstResponder {
    return YES;
}

- (BOOL)hasText {
	return YES;
}

- (void)deleteBackward {
}

- (void)onUpArrow {
	onUpArrow(self.handle);
}

- (void)onDownArrow {
	onDownArrow(self.handle);
}

- (void)onLeftArrow {
	onLeftArrow(self.handle);
}

- (void)onRightArrow {
	onRightArrow(self.handle);
}

- (NSArray<UIKeyCommand *> *)keyCommands {
	if (_keyCommands == nil) {
		_keyCommands = @[
			[UIKeyCommand keyCommandWithInput:UIKeyInputUpArrow
								modifierFlags:0
									   action:@selector(onUpArrow)],
			[UIKeyCommand keyCommandWithInput:UIKeyInputDownArrow
								modifierFlags:0
									   action:@selector(onDownArrow)],
			[UIKeyCommand keyCommandWithInput:UIKeyInputLeftArrow
								modifierFlags:0
									   action:@selector(onLeftArrow)],
			[UIKeyCommand keyCommandWithInput:UIKeyInputRightArrow
								modifierFlags:0
									   action:@selector(onRightArrow)]
		];
	}
	return _keyCommands;
}
@end

CFTypeRef gio_createDisplayLink(void) {
	CADisplayLink *dl = [CADisplayLink displayLinkWithTarget:[GioView class] selector:@selector(onFrameCallback:)];
	dl.paused = YES;
	NSRunLoop *runLoop = [NSRunLoop mainRunLoop];
	[dl addToRunLoop:runLoop forMode:[runLoop currentMode]];
	return (__bridge_retained CFTypeRef)dl;
}

int gio_startDisplayLink(CFTypeRef dlref) {
	CADisplayLink *dl = (__bridge CADisplayLink *)dlref;
	dl.paused = NO;
	return 0;
}

int gio_stopDisplayLink(CFTypeRef dlref) {
	CADisplayLink *dl = (__bridge CADisplayLink *)dlref;
	dl.paused = YES;
	return 0;
}

void gio_releaseDisplayLink(CFTypeRef dlref) {
	CADisplayLink *dl = (__bridge CADisplayLink *)dlref;
	[dl invalidate];
	CFRelease(dlref);
}

void gio_setDisplayLinkDisplay(CFTypeRef dl, uint64_t did) {
	// Nothing to do on iOS.
}

void gio_hideCursor() {
	// Not supported.
}

void gio_showCursor() {
	// Not supported.
}

void gio_setCursor(NSUInteger curID) {
	// Not supported.
}

void gio_viewSetHandle(CFTypeRef viewRef, uintptr_t handle) {
	GioView *v = (__bridge GioView *)viewRef;
	v.handle = handle;
}

@interface _gioAppDelegate : UIResponder <UIApplicationDelegate>
@property (strong, nonatomic) UIWindow *window;
@end

@implementation _gioAppDelegate
- (BOOL)application:(UIApplication *)application didFinishLaunchingWithOptions:(NSDictionary *)launchOptions {
	self.window = [[UIWindow alloc] initWithFrame:[[UIScreen mainScreen] bounds]];
	GioViewController *controller = [[GioViewController alloc] initWithNibName:nil bundle:nil];
	self.window.rootViewController = controller;
	[self.window makeKeyAndVisible];
	return YES;
}
- (BOOL)application:(UIApplication *)app openURL:(NSURL *)url options:(NSDictionary<UIApplicationOpenURLOptionsKey, id> *)options {
    gio_onOpenURI((__bridge CFTypeRef)url.absoluteString);
	return YES;
}
@end

int gio_applicationMain(int argc, char *argv[]) {
	@autoreleasepool {
		return UIApplicationMain(argc, argv, nil, NSStringFromClass([_gioAppDelegate class]));
	}
}
